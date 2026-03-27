package claimcheck_test

import (
	"testing"

	"github.com/pmferg/ClaimCheckMQTT/pkg/claimcheck"
)

func TestGenerateMPID_Unique(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := claimcheck.GenerateMPID()
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate MPID on iteration %d: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestEnvelope_RoundTrip(t *testing.T) {
	env := claimcheck.NewEnvelope("devices/temp", "sensor-1", "application/octet-stream", 4096)
	data, err := env.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := claimcheck.UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.MPID != env.MPID {
		t.Errorf("MPID mismatch: got %s, want %s", got.MPID, env.MPID)
	}
	if got.Type != claimcheck.EnvelopeType {
		t.Errorf("type mismatch: got %s", got.Type)
	}
}

func TestEnvelope_CallbackTopic(t *testing.T) {
	env := claimcheck.NewEnvelope("t", "src", "text/plain", 10)
	env.CallbackTopic = "claimcheck/callbacks/abc-123"

	data, _ := env.Marshal()
	got, err := claimcheck.UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CallbackTopic != "claimcheck/callbacks/abc-123" {
		t.Errorf("callback_topic mismatch: got %q", got.CallbackTopic)
	}
}

func TestIsClaimCheck(t *testing.T) {
	env := claimcheck.NewEnvelope("t", "", "text/plain", 10)
	data, _ := env.Marshal()

	if !claimcheck.IsClaimCheck(data) {
		t.Error("expected IsClaimCheck to return true for a valid envelope")
	}
	if claimcheck.IsClaimCheck([]byte(`{"type":"other"}`)) {
		t.Error("expected IsClaimCheck to return false for non-claimcheck type")
	}
	if claimcheck.IsClaimCheck([]byte(`not json`)) {
		t.Error("expected IsClaimCheck to return false for invalid JSON")
	}
}

func TestIsClaimCheck_CallbackClose(t *testing.T) {
	env := claimcheck.NewCallbackCloseEnvelope("claimcheck/callbacks/xyz", "src")
	data, _ := env.Marshal()

	if !claimcheck.IsClaimCheck(data) {
		t.Error("expected IsClaimCheck true for callback close")
	}
	if !claimcheck.IsCallbackClose(data) {
		t.Error("expected IsCallbackClose true")
	}
}

func TestIsCallbackClose_FalseForNormal(t *testing.T) {
	env := claimcheck.NewEnvelope("t", "", "text/plain", 5)
	data, _ := env.Marshal()

	if claimcheck.IsCallbackClose(data) {
		t.Error("expected IsCallbackClose false for normal envelope")
	}
}
