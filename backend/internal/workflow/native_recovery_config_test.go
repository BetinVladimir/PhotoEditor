package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeRecoveryLimits(t *testing.T) {
	valid := NativeRecoveryLimits{SchemaVersion: 1, RAM: 800, VRAM: 100, Scratch: 100, Output: 100, Metadata: 100, Reserved: 1, TimeoutSeconds: 1740, Manual: NativeManualLimits{1, 1, 1}, ManualBatch: NativeManualLimits{2, 2, 2}}
	body, _ := json.Marshal(valid)
	if _, err := DecodeNativeRecoveryLimits(body); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*NativeRecoveryLimits){func(c *NativeRecoveryLimits) { c.SchemaVersion = 2 }, func(c *NativeRecoveryLimits) { c.Metadata = 101 }, func(c *NativeRecoveryLimits) { c.TimeoutSeconds = 1741 }, func(c *NativeRecoveryLimits) { c.ManualBatch.PayloadBytes = 0 }, func(c *NativeRecoveryLimits) { c.RAM = 9007199254740992 }} {
		c := valid
		mutate(&c)
		b, _ := json.Marshal(c)
		if _, err := DecodeNativeRecoveryLimits(b); err == nil {
			t.Fatal("accepted invalid limits")
		}
	}
	for _, b := range []string{"null", "{}", string(body) + " {}", strings.Repeat(" ", 16385), strings.TrimSuffix(string(body), "}") + `,"qualification_id":"untrusted"}`} {
		if _, err := DecodeNativeRecoveryLimits([]byte(b)); err == nil {
			t.Fatal("accepted malformed configuration")
		}
	}
}
func TestNativeRecoveryMissingConfiguration(t *testing.T) {
	if _, err := LoadNativeRecoveryFactory("/nonexistent/native-recovery.json", "secret-dsn", ""); err == nil || strings.Contains(err.Error(), "secret-dsn") {
		t.Fatal("missing configuration must fail safely")
	}
}
