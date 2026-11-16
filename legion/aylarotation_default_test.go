package main

import (
	"encoding/json"
	"testing"
)

// AY15 (2026-11-15): the rotation experiment is stopped. The code default must be OFF, so a tuning.json without
// the key (or a missing file) never turns Ayla's member rotation on.
func TestAylaRotationDefaultOff(t *testing.T) {
	if !defaultTuning.AylaRotationOff {
		t.Fatal("defaultTuning.AylaRotationOff = false: rotation would be ON by default (AY15 says OFF)")
	}

	// a tuning.json that does not mention the key keeps the default
	nt := defaultTuning
	if err := json.Unmarshal([]byte(`{"party_lv_spread": 20}`), &nt); err != nil {
		t.Fatal(err)
	}
	if !nt.AylaRotationOff {
		t.Fatal("a tuning.json without ayla_rotation_off turned rotation on")
	}

	// the owner can still switch it on explicitly
	on := defaultTuning
	if err := json.Unmarshal([]byte(`{"ayla_rotation_off": false}`), &on); err != nil {
		t.Fatal(err)
	}
	if on.AylaRotationOff {
		t.Fatal("ayla_rotation_off false did not turn rotation on")
	}
}
