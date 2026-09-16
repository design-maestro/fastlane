package store

import "path/filepath"

// Paths defines the persisted file layout.
type Paths struct {
	Root              string
	LockPath          string
	SubscriptionsPath string
	SettingsPath      string
	StatePath         string
	AWGProfilePath    string
	AWGProfilesPath   string
	AWGProfilesDir    string
}

// NewPaths constructs the default file layout for a root directory.
func NewPaths(root string) Paths {
	return Paths{
		Root:              root,
		LockPath:          filepath.Join(root, ".fastlane.lock"),
		SubscriptionsPath: filepath.Join(root, "subscriptions.json"),
		SettingsPath:      filepath.Join(root, "settings.json"),
		StatePath:         filepath.Join(root, "state.json"),
		AWGProfilePath:    filepath.Join(root, "amneziawg.conf"),
		AWGProfilesPath:   filepath.Join(root, "amneziawg-profiles.json"),
		AWGProfilesDir:    filepath.Join(root, "amneziawg.d"),
	}
}
