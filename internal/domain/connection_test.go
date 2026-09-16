package domain

import "testing"

func TestDefaultRuntimeStateUsesDirectOperationalMode(t *testing.T) {
	t.Parallel()

	state := DefaultRuntimeState()
	if state.SchemaVersion != 5 {
		t.Fatalf("unexpected schema version: %d", state.SchemaVersion)
	}
	if state.OperationalMode != OperationalModeDirect {
		t.Fatalf("unexpected operational mode: %s", state.OperationalMode)
	}
}

func TestLegacyOperationalMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		connected bool
		transport TransportMode
		want      OperationalMode
	}{
		{name: "connected proxy", connected: true, transport: TransportModeProxy, want: OperationalModeVPN},
		{name: "disconnected proxy", connected: false, transport: TransportModeProxy, want: OperationalModeDirect},
		{name: "connected direct", connected: true, transport: TransportModeDirect, want: OperationalModeDirect},
		{name: "connected zapret", connected: true, transport: TransportModeZapret, want: OperationalModeDirect},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := LegacyOperationalMode(tt.connected, tt.transport); got != tt.want {
				t.Fatalf("LegacyOperationalMode() = %s, want %s", got, tt.want)
			}
		})
	}
}
