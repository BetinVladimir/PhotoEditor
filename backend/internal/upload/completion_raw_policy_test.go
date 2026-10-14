package upload

import (
	"context"
	"strings"
	"testing"
	"time"

	"photoeditor/backend/internal/rawdevelopment"
)

func TestCompletionRAWPolicyRejectsUnknownOrInvalidFields(t *testing.T) {
	valid := `{"schema_version":1,"demosaic":"ahd","wb_source":"camera","exposure_ev":0,"highlight_policy":"clip","decoder_version":"rawpy-0.20.0/libraw-0.21.0"}`
	policy, err := decodeCompletionRAWPolicy([]byte(valid))
	if err != nil || policy == nil || !rawdevelopment.Valid(policy) {
		t.Fatal("valid persisted RAW policy rejected", err)
	}
	if absent, err := decodeCompletionRAWPolicy(nil); err != nil || absent != nil {
		t.Fatal("legacy upload must retain nil RAW policy", err)
	}
	for _, raw := range []string{
		strings.TrimSuffix(valid, "}") + `,"unknown":"silently dropped"}`,
		strings.Replace(valid, `"demosaic":"ahd"`, `"demosaic":"foreign"`, 1),
		valid + ` {}`,
		`null`,
	} {
		if _, err := decodeCompletionRAWPolicy([]byte(raw)); err == nil {
			t.Fatal("invalid RAW policy accepted", raw)
		}
	}
}

func TestCompletionRejectsInvalidRAWPolicyBeforeObjectIO(t *testing.T) {
	lease, pending, object := completionFixture()
	lease.RAWDevelopment = &rawdevelopment.Policy{SchemaVersion: 1}
	leasing := &completionLeaseFake{lease: &lease, renew: true}
	objects := &completionObjectFake{pending: pending, object: object}
	changed, err := (CompletionProcessor{Leases: leasing, Objects: objects,
		HeartbeatInterval: 10 * time.Millisecond, RenewTimeout: 10 * time.Millisecond,
		WorkTimeout: time.Second}).ProcessOne(context.Background())
	if changed || err == nil || objects.completes != 0 || objects.verifies != 0 || leasing.releases != 1 {
		t.Fatal("invalid RAW policy crossed object IO boundary", changed, err)
	}
}
