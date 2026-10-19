package app

import (
	"context"
	"fmt"

	"github.com/design-maestro/fastlane/internal/domain"
)

// ResumeConnection restores the previous server without rescanning the pool.
func (s *Service) ResumeConnection(ctx context.Context) error {
	state, err := s.store.LoadState()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	if state.Connected {
		return nil
	}
	if state.ResumeConnection == nil {
		_, err := s.ConnectAuto(ctx, "")
		return err
	}
	return runStoreWriteLocked(s, func() error {
		state, err := s.store.LoadState()
		if err != nil {
			return fmt.Errorf("load state: %w", err)
		}
		if state.Connected {
			return nil
		}
		resume := state.ResumeConnection
		if resume == nil {
			return fmt.Errorf("previous connection is unavailable; choose a server from the list")
		}
		if resume.AWGProfileID != "" {
			return s.connectAWGWithModeLocked(ctx, resume.AWGProfileID, resume.Mode, resume.AutoScope)
		}
		sub, err := s.subscriptionByID(resume.SubscriptionID)
		if err != nil {
			return err
		}
		if sub.IsExpired(s.currentTime().UTC()) {
			return fmt.Errorf("subscription %q expired; its servers are view-only", sub.ID)
		}
		node, ok := sub.NodeByID(resume.NodeID)
		if !ok {
			return fmt.Errorf("previous server is unavailable; choose a server from the list")
		}
		settings, err := s.store.LoadSettings()
		if err != nil {
			return err
		}
		state.AutoScope = resume.AutoScope
		if err := s.saveState(state); err != nil {
			return err
		}
		if err := s.applyNodeSelection(ctx, sub, node, resume.Mode, selectionOptionsForState(state)); err != nil {
			return err
		}
		settings.Mode = resume.Mode
		settings.AutoMode = resume.Mode == domain.SelectionModeAuto
		return s.store.SaveSettings(settings)
	})
}
