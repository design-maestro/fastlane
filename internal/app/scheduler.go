package app

import (
	"context"
	"sync"
	"time"
)

const (
	maxRefreshConfigPollInterval = time.Second
	maxHealthConfigPollInterval  = time.Second
	connectionWatchInterval      = 15 * time.Second
)

// Scheduler periodically refreshes subscriptions using the global settings interval.
type Scheduler struct {
	service                *Service
	now                    func() time.Time
	tick                   time.Duration
	stopCh                 chan struct{}
	healthCheck            func(context.Context)
	healthTrigger          func() (string, bool)
	triggeredHealthCheck   func(context.Context, string)
	recoveryCheck          func(context.Context) (bool, string, error)
	healthMu               sync.Mutex
	refreshConfigPollEvery time.Duration
	healthConfigPollEvery  time.Duration

	lastRefreshLoopConfigErr string
	lastHealthLoopConfigErr  string
}

// NewScheduler creates a scheduler instance.
func NewScheduler(service *Service) *Scheduler {
	return &Scheduler{
		service: service,
		now:     time.Now,
		tick:    time.Minute,
		stopCh:  make(chan struct{}),
	}
}

// SetTick overrides the scheduler tick interval.
func (s *Scheduler) SetTick(tick time.Duration) {
	if tick > 0 {
		s.tick = tick
	}
}

// SetHealthCheck overrides the periodic health pass. It is primarily used by
// the daemon to persist user-visible background progress around the pass.
func (s *Scheduler) SetHealthCheck(check func(context.Context)) {
	s.healthCheck = check
}

// SetHealthTrigger configures a durable external trigger and its worker. The
// trigger is polled by the daemon, so a LuCI request may return immediately and
// the health pass continues even after the browser leaves the page.
func (s *Scheduler) SetHealthTrigger(trigger func() (string, bool), check func(context.Context, string)) {
	s.healthTrigger = trigger
	s.triggeredHealthCheck = check
}

// Start begins the background refresh loop.
func (s *Scheduler) Start(ctx context.Context) {
	go s.runRefreshLoop(ctx)
	go s.runHealthLoop(ctx)
	go s.runConnectionWatchLoop(ctx)
}

// Stop terminates the background loop.
func (s *Scheduler) Stop() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

// RunOnce performs a single refresh scan across stored subscriptions.
func (s *Scheduler) RunOnce(ctx context.Context) {
	s.runOnce(ctx)
}

// RunHealthOnce performs a single health-monitoring pass for auto mode.
func (s *Scheduler) RunHealthOnce(ctx context.Context) {
	s.runHealthOnce(ctx)
}

func (s *Scheduler) runRefreshLoop(ctx context.Context) {
	s.RunOnce(ctx)
	lastScanAt := s.now()
	lastInterval, enabled := s.refreshLoopConfig()
	for {
		waitFor := s.refreshConfigPollInterval()
		if enabled {
			scanEvery := s.refreshScanInterval(lastInterval)
			untilDue := lastScanAt.Add(scanEvery).Sub(s.now())
			if untilDue > 0 && untilDue < waitFor {
				waitFor = untilDue
			}
		}
		if !s.wait(ctx, waitFor) {
			return
		}

		interval, currentlyEnabled := s.refreshLoopConfig()
		now := s.now()
		configChanged := interval != lastInterval || currentlyEnabled != enabled
		if currentlyEnabled && (configChanged || !now.Before(lastScanAt.Add(s.refreshScanInterval(interval)))) {
			s.runOnce(ctx)
			lastScanAt = s.now()
		}
		lastInterval = interval
		enabled = currentlyEnabled
	}
}

func (s *Scheduler) runHealthLoop(ctx context.Context) {
	// Keep the cadence anchored to the last completed pass (or scheduler start),
	// but re-read the setting frequently. SetSetting may run in another process
	// on OpenWrt, so an in-memory wake-up alone would not see CLI/LuCI changes.
	lastRunAt := s.now()
	for {
		if scope, triggered := s.consumeHealthTrigger(); triggered {
			s.runTriggeredHealthOnce(ctx, scope)
			lastRunAt = s.now()
			continue
		}
		interval, enabled := s.healthLoopConfig()
		now := s.now()
		if enabled && !now.Before(lastRunAt.Add(interval)) {
			s.runHealthOnce(ctx)
			lastRunAt = s.now()
			continue
		}

		waitFor := s.healthConfigPollInterval()
		if enabled {
			untilDue := lastRunAt.Add(interval).Sub(now)
			if untilDue > 0 && untilDue < waitFor {
				waitFor = untilDue
			}
		}
		if !s.wait(ctx, waitFor) {
			return
		}
	}
}

func (s *Scheduler) consumeHealthTrigger() (string, bool) {
	if s.healthTrigger == nil {
		return "", false
	}
	return s.healthTrigger()
}

func (s *Scheduler) runConnectionWatchLoop(ctx context.Context) {
	for s.wait(ctx, connectionWatchInterval) {
		s.runConnectionWatchOnce(ctx)
	}
}

func (s *Scheduler) runConnectionWatchOnce(ctx context.Context) {
	// A full URL-test pass temporarily changes probe processes and may take
	// longer than the watch interval. Do not queue a second pass from a stale
	// recovery observation while one is already running.
	if !s.healthMu.TryLock() {
		return
	}
	defer s.healthMu.Unlock()

	var needed bool
	var reason string
	var err error
	if s.recoveryCheck != nil {
		needed, reason, err = s.recoveryCheck(ctx)
	} else if s.service != nil {
		needed, reason, err = s.service.AutoRecoveryNeeded(ctx)
	}
	if err != nil {
		s.logWarn("check active auto route", "error", err.Error())
		return
	}
	if !needed {
		return
	}
	s.logWarn("active auto route failed; starting immediate reselection", "reason", reason)
	s.runHealthOnceLocked(ctx)
}

func (s *Scheduler) runOnce(ctx context.Context) {
	settings, err := s.service.GetSettings()
	if err != nil {
		s.logWarn("load refresh interval for scheduler", "error", err.Error())
		return
	}
	interval := settings.RefreshInterval.Duration()
	if interval <= 0 {
		return
	}

	subscriptions, err := s.service.ListSubscriptions()
	if err != nil {
		s.logWarn("list subscriptions for scheduler", "error", err.Error())
		return
	}

	status, statusErr := s.service.Status()
	activeSubscriptionID := ""
	connected := false
	lastRefreshAt := map[string]time.Time{}
	if statusErr == nil {
		activeSubscriptionID = status.State.ActiveSubscriptionID
		connected = status.State.Connected
		lastRefreshAt = status.State.LastRefreshAt
	}

	for _, sub := range subscriptions {
		if sub.IsExpired(s.now().UTC()) {
			continue
		}
		lastAttempt := sub.LastUpdatedAt
		if refreshedAt, ok := lastRefreshAt[sub.ID]; ok && !refreshedAt.IsZero() {
			lastAttempt = refreshedAt
		}
		now := s.now().UTC()
		if now.Sub(lastAttempt) < interval {
			continue
		}
		if err := s.service.touchRefreshAttempt(sub.ID, now); err != nil {
			s.logWarn("record refresh attempt", "subscription", sub.ID, "error", err.Error())
		}

		if connected && sub.ID == activeSubscriptionID {
			if err := s.service.RefreshAndReconnect(ctx); err != nil {
				s.logWarn("refresh and reconnect active subscription", "subscription", sub.ID, "error", err.Error())
				continue
			}
			s.logInfo("refreshed and reconnected active subscription", "subscription", sub.ID)
			continue
		}

		if _, err := s.service.RefreshSubscription(ctx, sub.ID); err != nil {
			s.logWarn("refresh subscription", "subscription", sub.ID, "error", err.Error())
			continue
		}
		s.logInfo("refreshed subscription", "subscription", sub.ID)
	}
}

func (s *Scheduler) runHealthOnce(ctx context.Context) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	s.runHealthOnceLocked(ctx)
}

func (s *Scheduler) runHealthOnceLocked(ctx context.Context) {
	if s.healthCheck != nil {
		s.healthCheck(ctx)
		return
	}
	if err := s.service.RunAutoHealthCheck(ctx); err != nil {
		s.logWarn("auto health check", "error", err.Error())
	}
}

func (s *Scheduler) runTriggeredHealthOnce(ctx context.Context, scope string) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if s.triggeredHealthCheck != nil {
		s.triggeredHealthCheck(ctx, scope)
		return
	}
	if s.healthCheck != nil {
		s.healthCheck(ctx)
		return
	}
	if err := s.service.RunAutoHealthCheck(ctx); err != nil {
		s.logWarn("triggered auto health check", "error", err.Error())
	}
}

func (s *Scheduler) healthConfigPollInterval() time.Duration {
	if s.healthConfigPollEvery > 0 {
		return s.healthConfigPollEvery
	}
	pollInterval := maxHealthConfigPollInterval
	if s.tick > 0 && s.tick < pollInterval {
		pollInterval = s.tick
	}
	return pollInterval
}

func (s *Scheduler) refreshConfigPollInterval() time.Duration {
	if s.refreshConfigPollEvery > 0 {
		return s.refreshConfigPollEvery
	}
	pollInterval := maxRefreshConfigPollInterval
	if s.tick > 0 && s.tick < pollInterval {
		pollInterval = s.tick
	}
	return pollInterval
}

func (s *Scheduler) refreshScanInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return time.Minute
	}
	if s.tick > 0 && s.tick < interval {
		return s.tick
	}
	return interval
}

func (s *Scheduler) refreshLoopConfig() (time.Duration, bool) {
	if s.service == nil || s.service.store == nil {
		return time.Minute, false
	}

	settings, err := s.service.store.LoadSettings()
	if err != nil {
		s.logRefreshLoopConfigError(err)
		return time.Minute, false
	}
	s.lastRefreshLoopConfigErr = ""

	interval := settings.RefreshInterval.Duration()
	return interval, interval > 0
}

func (s *Scheduler) healthLoopConfig() (time.Duration, bool) {
	if s.service == nil || s.service.store == nil {
		return time.Minute, false
	}

	settings, err := s.service.store.LoadSettings()
	if err != nil {
		s.logHealthLoopConfigError(err)
		return time.Minute, false
	}
	s.lastHealthLoopConfigErr = ""

	interval := settings.HealthCheckInterval.Duration()
	if interval > 0 {
		return interval, true
	}
	if s.tick > 0 {
		return s.tick, false
	}
	return time.Minute, false
}

func (s *Scheduler) wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		d = time.Minute
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-s.stopCh:
		return false
	case <-timer.C:
		return true
	}
}

func (s *Scheduler) logInfo(msg string, args ...any) {
	if s.service != nil && s.service.logger != nil {
		s.service.logger.Info(msg, args...)
	}
}

func (s *Scheduler) logWarn(msg string, args ...any) {
	if s.service != nil && s.service.logger != nil {
		s.service.logger.Warn(msg, args...)
	}
}

func (s *Scheduler) logHealthLoopConfigError(err error) {
	if err == nil {
		return
	}

	message := err.Error()
	if s.lastHealthLoopConfigErr == message {
		return
	}

	s.lastHealthLoopConfigErr = message
	s.logWarn("load settings for auto health loop", "error", message)
}

func (s *Scheduler) logRefreshLoopConfigError(err error) {
	if err == nil {
		return
	}

	message := err.Error()
	if s.lastRefreshLoopConfigErr == message {
		return
	}

	s.lastRefreshLoopConfigErr = message
	s.logWarn("load settings for subscription refresh loop", "error", message)
}
