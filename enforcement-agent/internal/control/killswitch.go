package control

import "sync/atomic"

// KillSwitch manages a runtime enforcement toggle.
type KillSwitch struct {
	state atomic.Bool
}

// NewKillSwitch creates a kill switch with the provided default state.
func NewKillSwitch(enabled bool) *KillSwitch {
	ks := &KillSwitch{}
	ks.state.Store(enabled)
	return ks
}

// Enable disables enforcement.
func (k *KillSwitch) Enable() {
	k.state.Store(true)
}

// Disable restores enforcement.
func (k *KillSwitch) Disable() {
	k.state.Store(false)
}

// Enabled reports whether enforcement is currently disabled.
func (k *KillSwitch) Enabled() bool {
	return k.state.Load()
}

// Set toggles the state directly.
func (k *KillSwitch) Set(enabled bool) {
	if enabled {
		k.Enable()
	} else {
		k.Disable()
	}
}
