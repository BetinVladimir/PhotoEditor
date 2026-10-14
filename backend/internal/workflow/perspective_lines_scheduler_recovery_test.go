package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

func TestPerspectiveLinesRecoveryReconstructsOnlyExactSavedIntentAndPolicy(t *testing.T) {
	f := newLinesInputsFixture(t, true)
	snapshot := uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	intent, err := CompilePerspectiveLinesIntent(f.plan, snapshot, who)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := CompilePerspectiveLinesExecutionPolicy(f.plan.workingICC, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	makeRaw := func(i json.RawMessage, p string) []byte {
		raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "intent": i, "policy_jcs": p})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := makeRaw(intent, string(policy.Bytes()))
	plan, gotSnapshot, gotWho, err := parsePerspectiveLinesRecovery(raw, f.plan.scope.Tenant, f.plan.scope.Run, policy)
	if err != nil || plan.task != f.plan.task || gotSnapshot != snapshot || gotWho.UserID != who.UserID || gotWho.SessionID != who.SessionID {
		t.Fatal("recovery lost saved authority", err)
	}
	var changed map[string]any
	if err := json.Unmarshal(intent, &changed); err != nil {
		t.Fatal(err)
	}
	changed["task_id"] = uuid.New().String()
	forged, err := canonical.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	cases := [][]byte{nil, []byte("null"), makeRaw(forged, string(policy.Bytes())), makeRaw(intent, "{}")}
	for _, body := range cases {
		if _, _, _, err := parsePerspectiveLinesRecovery(body, f.plan.scope.Tenant, f.plan.scope.Run, policy); err == nil {
			t.Fatal("forged recovery accepted")
		}
	}
	if _, _, _, err := parsePerspectiveLinesRecovery(raw, uuid.New(), f.plan.scope.Run, policy); err == nil {
		t.Fatal("foreign tenant accepted")
	}
	if _, _, _, err := parsePerspectiveLinesRecovery(raw, f.plan.scope.Tenant, uuid.New(), policy); err == nil {
		t.Fatal("foreign run accepted")
	}
	foreign, _ := CompilePerspectiveLinesExecutionPolicy(f.plan.workingICC, strings.Repeat("b", 64))
	if _, _, _, err := parsePerspectiveLinesRecovery(raw, f.plan.scope.Tenant, f.plan.scope.Run, foreign); err == nil {
		t.Fatal("changed deployment policy accepted")
	}
}
func TestPerspectiveLinesResumeSealedRejectsCancellationAndMissingDependencies(t *testing.T) {
	s := PerspectiveLinesPreparation{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.ResumeSealed(ctx, uuid.New(), uuid.New()); err != context.Canceled {
		t.Fatal(err)
	}
	if err := s.ResumeSealed(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("missing dependencies accepted")
	}
}
