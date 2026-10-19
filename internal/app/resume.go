package app

import (
	"context"
	"fmt"

	"github.com/design-maestro/fastlane/internal/domain"
)

// ResumeConnection restores the last explicitly disconnected selection.
func (s *Service) ResumeConnection(ctx context.Context) error {
	state, err := s.store.LoadState()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	if state.Connected {
		return nil
	}
	resume := state.ResumeConnection
	if resume == nil || resume.Mode == domain.SelectionModeAuto {
		scope := ""
		if resume != nil && resume.AutoScope != autoScopeAll {
			scope = resume.SubscriptionID
		}
		_, err := s.ConnectAuto(ctx, scope)
		return err
	}
	if resume.AWGProfileID != "" {
		return s.ConnectAWGProfile(ctx, resume.AWGProfileID)
	}
	if resume.SubscriptionID == "" || resume.NodeID == "" {
		return fmt.Errorf("previous server is unavailable; choose a server from the list")
	}
	return s.ConnectManual(ctx, resume.SubscriptionID, resume.NodeID)
}
