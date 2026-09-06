package app

import "context"

// RefreshAndReconnect refreshes the current subscription and reapplies the active mode.
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

		switch status.State.Mode {
		case "manual":
			refreshedStatus, statusErr := s.Status()
			if statusErr != nil {
				return statusErr
			}
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
