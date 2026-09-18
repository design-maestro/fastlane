package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

const managedReserveScoreEquivalence = 100.0
const managedReservePromotionAbsolute = 50.0
const managedReservePromotionRelative = 0.20
const managedReservePromotionConfirmations = 2

type managedReserveRankedCandidate struct {
	candidate  managedReserveCandidate
	score      domain.ScoreResult
	hasHistory bool
}

// rankManagedReserveCandidates returns a new, deterministic candidate order.
// Known healthy nodes lead unknown nodes, and known unhealthy nodes trail them.
// Within each tier, the probe score balances latency and reliability history.
func rankManagedReserveCandidates(candidates []managedReserveCandidate, health map[string]domain.NodeHealth) []managedReserveCandidate {
	cfg := probe.DefaultScoreConfig()
	ranked := make([]managedReserveRankedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		nodeHealth, hasHistory := health[candidate.node.ID]
		nodeHealth.NodeID = candidate.node.ID
		ranked = append(ranked, managedReserveRankedCandidate{
			candidate:  candidate,
			score:      probe.CalculateScore(nodeHealth, cfg),
			hasHistory: hasHistory,
		})
	}

	sort.Slice(ranked, func(i, j int) bool {
		left, right := ranked[i], ranked[j]
		if leftTier, rightTier := managedReserveHealthTier(left), managedReserveHealthTier(right); leftTier != rightTier {
			return leftTier < rightTier
		}
		if left.score.Score != right.score.Score {
			return left.score.Score > right.score.Score
		}
		if left.candidate.sub.ID != right.candidate.sub.ID {
			return left.candidate.sub.ID < right.candidate.sub.ID
		}
		if left.candidate.node.ID != right.candidate.node.ID {
			return left.candidate.node.ID < right.candidate.node.ID
		}
		return left.candidate.tag < right.candidate.tag
	})

	preferEquivalentDifferentSubscription(ranked)

	ordered := make([]managedReserveCandidate, len(ranked))
	for i := range ranked {
		ordered[i] = ranked[i].candidate
	}
	return ordered
}

func collectManagedReserveCandidates(subscriptions []domain.Subscription, settings domain.Settings, state domain.RuntimeState, now time.Time) []managedReserveCandidate {
	candidates := make([]managedReserveCandidate, 0)
	seenConnections := make(map[string]struct{})
	for _, sub := range subscriptions {
		if sub.IsExpired(now) {
			continue
		}
		for _, node := range sub.Nodes {
			if state.OperationalMode != domain.OperationalModeDirect && sub.ID == state.ActiveSubscriptionID && node.ID == state.ActiveNodeID {
				continue
			}
			if domain.IsNodeExcludedFromAuto(settings, sub.ID, node) {
				continue
			}
			fingerprint := managedReserveConnectionFingerprint(node)
			if _, duplicate := seenConnections[fingerprint]; duplicate {
				continue
			}
			seenConnections[fingerprint] = struct{}{}
			candidates = append(candidates, managedReserveCandidate{sub: sub, node: node})
		}
	}
	return candidates
}

func managedReserveConnectionFingerprint(node domain.Node) string {
	node.ID = ""
	node.SubscriptionID = ""
	node.Name = ""
	node.ProviderName = ""
	node.Remark = ""
	if len(node.RawOutbound) > 0 {
		var raw map[string]any
		if json.Unmarshal(node.RawOutbound, &raw) == nil {
			delete(raw, "tag")
			if normalized, err := json.Marshal(raw); err == nil {
				node.RawOutbound = normalized
			}
		}
	}
	return domain.StableNodeID(node)
}

func existingManagedReserveCandidates(outbounds []domain.RuntimeOutboundState, eligible []managedReserveCandidate) []managedReserveCandidate {
	byNode := make(map[string]managedReserveCandidate, len(eligible))
	for _, candidate := range eligible {
		byNode[domain.AutoExcludedNodeKey(candidate.sub.ID, candidate.node.ID)] = candidate
	}
	existing := make([]managedReserveCandidate, 0, managedReserveLimit)
	seen := make(map[string]struct{})
	for _, outbound := range outbounds {
		if outbound.Role != "reserve" && outbound.Role != "candidate" {
			continue
		}
		key := domain.AutoExcludedNodeKey(outbound.SubscriptionID, outbound.NodeID)
		candidate, ok := byNode[key]
		if !ok {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		candidate.tag = outbound.Tag
		candidate.verifiedAt = outbound.VerifiedAt
		existing = append(existing, candidate)
	}
	return existing
}

func runtimeOutboundTags(outbounds []domain.RuntimeOutboundState) map[string]struct{} {
	tags := make(map[string]struct{}, len(outbounds))
	for _, outbound := range outbounds {
		if outbound.Tag != "" {
			tags[outbound.Tag] = struct{}{}
		}
	}
	return tags
}

func managedReserveProbeOrder(existing, ranked []managedReserveCandidate, health map[string]domain.NodeHealth, now time.Time) []managedReserveCandidate {
	ordered := append([]managedReserveCandidate(nil), existing...)
	measured := make([]managedReserveCandidate, 0, len(ranked))
	unknown := make([]managedReserveCandidate, 0, len(ranked))
	for _, candidate := range ranked {
		if containsManagedCandidate(ordered, candidate.tag) {
			continue
		}
		if _, ok := health[candidate.node.ID]; ok {
			measured = append(measured, candidate)
		} else {
			unknown = append(unknown, candidate)
		}
	}
	measuredSlots := managedReserveProbeLimit - len(ordered)
	if len(unknown) > 0 && measuredSlots > 0 {
		measuredSlots--
	}
	if measuredSlots > len(measured) {
		measuredSlots = len(measured)
	}
	ordered = append(ordered, measured[:measuredSlots]...)
	if len(unknown) > 0 {
		offset := int((now.Unix() / int64(time.Minute/time.Second)) % int64(len(unknown)))
		ordered = append(ordered, unknown[offset:]...)
		ordered = append(ordered, unknown[:offset]...)
	}
	ordered = append(ordered, measured[measuredSlots:]...)
	return ordered
}

func selectManagedReserveStates(successful []managedReserveCandidate, health map[string]domain.NodeHealth, previous []domain.RuntimeOutboundState, now time.Time) []domain.RuntimeOutboundState {
	confirmed := make([]managedReserveCandidate, 0, len(successful))
	for _, candidate := range successful {
		if health[candidate.node.ID].ConsecutiveSuccesses >= managedReserveHealthyConfirmations {
			confirmed = append(confirmed, candidate)
		}
	}
	if len(confirmed) == 0 {
		return nil
	}
	ranked := rankManagedReserveCandidates(confirmed, health)
	byTag := make(map[string]managedReserveCandidate, len(confirmed))
	for _, candidate := range confirmed {
		byTag[candidate.tag] = candidate
	}
	oldByTag := make(map[string]domain.RuntimeOutboundState)
	reserves := make([]managedReserveCandidate, 0, managedReserveLimit)
	for _, outbound := range previous {
		oldByTag[outbound.Tag] = outbound
		if outbound.Role == "reserve" {
			if candidate, ok := byTag[outbound.Tag]; ok && len(reserves) < managedReserveLimit {
				reserves = append(reserves, candidate)
			}
		}
	}
	for _, candidate := range ranked {
		if len(reserves) >= managedReserveLimit {
			break
		}
		if !containsManagedCandidate(reserves, candidate.tag) {
			reserves = append(reserves, candidate)
		}
	}

	var pending *managedReserveCandidate
	if len(reserves) == managedReserveLimit {
		weakest := 0
		if managedReserveScore(reserves[1], health).Score < managedReserveScore(reserves[0], health).Score {
			weakest = 1
		}
		for _, challenger := range ranked {
			if containsManagedCandidate(reserves, challenger.tag) {
				continue
			}
			meaningfulUpgrade := managedReserveMeaningfullyBetter(challenger, reserves[weakest], health)
			diversityUpgrade := reserves[0].sub.ID == reserves[1].sub.ID &&
				challenger.sub.ID != reserves[0].sub.ID &&
				managedReserveWithinDiversityWindow(challenger, reserves[weakest], health)
			if meaningfulUpgrade || diversityUpgrade {
				wins := oldByTag[challenger.tag].PromotionWins + 1
				if wins >= managedReservePromotionConfirmations {
					reserves[weakest] = challenger
				} else {
					candidate := challenger
					pending = &candidate
				}
				break
			}
		}
	}

	ordered := rankManagedReserveCandidates(reserves, health)
	states := make([]domain.RuntimeOutboundState, 0, managedReserveLimit+1)
	for index, candidate := range ordered {
		score := managedReserveScore(candidate, health)
		reason := "best reliability and latency score"
		if index == 1 && candidate.sub.ID != ordered[0].sub.ID {
			reason = "provider diversity within 100 ms score"
		}
		states = append(states, managedReserveState(candidate, score.Score, "reserve", reason, 0, health, now))
	}
	if pending != nil {
		score := managedReserveScore(*pending, health)
		wins := oldByTag[pending.tag].PromotionWins + 1
		states = append(states, managedReserveState(*pending, score.Score, "candidate", "awaiting second promotion confirmation", wins, health, now))
	}
	return states
}

func managedReserveWithinDiversityWindow(challenger, incumbent managedReserveCandidate, health map[string]domain.NodeHealth) bool {
	challengerScore := managedReserveScore(challenger, health)
	incumbentScore := managedReserveScore(incumbent, health)
	return challengerScore.Healthy && incumbentScore.Healthy && challengerScore.Score >= incumbentScore.Score-managedReserveScoreEquivalence
}

func managedReserveState(candidate managedReserveCandidate, score float64, role, reason string, wins int, health map[string]domain.NodeHealth, now time.Time) domain.RuntimeOutboundState {
	measurements := health[candidate.node.ID].SuccessCount + health[candidate.node.ID].FailureCount
	return domain.RuntimeOutboundState{
		Tag: candidate.tag, SubscriptionID: candidate.sub.ID, NodeID: candidate.node.ID,
		Role: role, VerifiedAt: now, Score: score, Samples: measurements,
		SelectionReason: reason, PromotionWins: wins,
	}
}

func managedReserveScore(candidate managedReserveCandidate, health map[string]domain.NodeHealth) domain.ScoreResult {
	nodeHealth := health[candidate.node.ID]
	nodeHealth.NodeID = candidate.node.ID
	return probe.CalculateScore(nodeHealth, probe.DefaultScoreConfig())
}

func managedReserveMeaningfullyBetter(challenger, incumbent managedReserveCandidate, health map[string]domain.NodeHealth) bool {
	challengerScore := managedReserveScore(challenger, health)
	incumbentScore := managedReserveScore(incumbent, health)
	if !challengerScore.Healthy || !incumbentScore.Healthy {
		return challengerScore.Healthy && !incumbentScore.Healthy
	}
	improvement := challengerScore.Score - incumbentScore.Score
	incumbentCost := probe.DefaultScoreConfig().HealthyBonus - incumbentScore.Score
	return improvement >= managedReservePromotionAbsolute && incumbentCost > 0 && improvement/incumbentCost >= managedReservePromotionRelative
}

func containsManagedCandidate(candidates []managedReserveCandidate, tag string) bool {
	for _, candidate := range candidates {
		if candidate.tag == tag {
			return true
		}
	}
	return false
}

func managedReserveDiagnosticLabel(state domain.RuntimeOutboundState) string {
	return fmt.Sprintf("score %.0f, %d samples, %s", state.Score, state.Samples, state.SelectionReason)
}

func managedReserveHealthTier(candidate managedReserveRankedCandidate) int {
	if candidate.hasHistory && candidate.score.Healthy {
		return 0
	}
	if !candidate.hasHistory {
		return 1
	}
	return 2
}

// The first candidate is always the score winner. For the second slot, provider
// diversity is worth at most 100 score points, equivalent to 100 ms under the
// default probe score. Candidates from another health tier are never equivalent.
func preferEquivalentDifferentSubscription(ranked []managedReserveRankedCandidate) {
	if len(ranked) < 3 || ranked[0].candidate.sub.ID != ranked[1].candidate.sub.ID {
		return
	}

	secondTier := managedReserveHealthTier(ranked[1])
	minimumEquivalentScore := ranked[1].score.Score - managedReserveScoreEquivalence
	for i := 2; i < len(ranked); i++ {
		candidate := ranked[i]
		if managedReserveHealthTier(candidate) != secondTier {
			break
		}
		if candidate.score.Score < minimumEquivalentScore {
			break
		}
		if candidate.candidate.sub.ID == ranked[0].candidate.sub.ID {
			continue
		}
		copy(ranked[2:i+1], ranked[1:i])
		ranked[1] = candidate
		return
	}
}
