package app

import (
	"bytes"
	"context"
	"reflect"

	"github.com/design-maestro/fastlane/internal/domain"
)

// RefreshAndReconnect refreshes the current subscription and reapplies the active
// mode only when the active node's runtime parameters changed.
func (s *Service) RefreshAndReconnect(ctx context.Context) error {
	var autoSnapshot autoSelectionSnapshot
	autoScope := ""
	err := runStoreWriteLocked(s, func() error {
		status, err := s.Status()
		if err != nil {
			return err
		}
		if status.State.ActiveSubscriptionID == "" {
			return nil
		}

		sub, err := s.refreshSubscription(ctx, status.State.ActiveSubscriptionID)
		if err != nil {
			return err
		}

		refreshedStatus, statusErr := s.Status()
		if statusErr != nil {
			return statusErr
		}
		if status.State.Connected && sameRuntimeNode(status.ActiveNode, refreshedStatus.ActiveNode) {
			s.logInfo("active subscription refreshed without reconnect", "subscription", sub.ID, "node", refreshedStatus.State.ActiveNodeID)
			return nil
		}

		switch status.State.Mode {
		case "manual":
			if refreshedStatus.State.ActiveNodeID == "" {
				return nil
			}
			return s.connectManual(ctx, sub.ID, refreshedStatus.State.ActiveNodeID)
		case "auto":
			autoScope = sub.ID
			if status.State.AutoScope == autoScopeAll {
				autoScope = autoScopeAll
			}
			autoSnapshot, err = s.captureAutoSelectionSnapshotLocked()
			return err
		default:
			return nil
		}
	})
	if err != nil || autoScope == "" {
		return err
	}

	_, err = s.connectAutoUsingSnapshot(ctx, autoScope, autoSnapshot)
	return err
}

func sameRuntimeNode(before, after *domain.Node) bool {
	if before == nil || after == nil {
		return false
	}
	if !sameRefreshedNodeIdentity(*before, *after) {
		return false
	}
	if (len(before.RawOutbound) > 0 || len(after.RawOutbound) > 0) && !bytes.Equal(before.RawOutbound, after.RawOutbound) {
		return false
	}
	if !reflect.DeepEqual(runtimeNodeExtras(before.Extras), runtimeNodeExtras(after.Extras)) {
		return false
	}
	if (before.Protocol == domain.ProtocolHysteria || before.Protocol == domain.ProtocolHysteria2) && before.RawQuery != after.RawQuery {
		return false
	}
	return true
}

func runtimeNodeExtras(extras map[string]string) map[string]string {
	result := make(map[string]string)
	for _, key := range []string{"insecure", "allowInsecure", "obfs", "obfs-password"} {
		if value := extras[key]; value != "" {
			result[key] = value
		}
	}
	return result
}
