package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"testing"
	"time"
)

func TestPerspectiveLinesSubmissionIntentRejectsClientOwnedExecutionFields(t *testing.T) {
	f := newLinesInputsFixture(t, false)
	intent := PerspectiveLinesSubmissionIntent{SchemaVersion: 1, Snapshot: uuid.New(), Base: f.plan.scope.Base}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodePerspectiveLinesSubmissionIntent(raw)
	if err != nil || got != intent {
		t.Fatal(got, err)
	}
	for _, extra := range []string{"runtime_lock_sha", "profile", "run_id", "source_jcs", "task_id"} {
		body, err := canonical.Marshal(map[string]any{"schema_version": 1, "snapshot_id": intent.Snapshot, "base": intent.Base, extra: "injected"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodePerspectiveLinesSubmissionIntent(body); err == nil {
			t.Fatal("execution injection accepted", extra)
		}
	}
	intent.Base.Source.ArtifactID = uuid.Nil
	bad, _ := canonical.Marshal(intent)
	if _, err := DecodePerspectiveLinesSubmissionIntent(bad); err == nil {
		t.Fatal("empty source accepted")
	}
}
func TestPerspectiveLinesSubmissionRunBindsKeyScopeAndActorNotSession(t *testing.T) {
	tenant, project := uuid.New(), uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	key := "perspective-lines-submission-0001"
	first, err := PerspectiveLinesSubmissionRun(tenant, project, who, key)
	if err != nil {
		t.Fatal(err)
	}
	who.SessionID = uuid.New()
	retry, err := PerspectiveLinesSubmissionRun(tenant, project, who, key)
	if err != nil || retry != first {
		t.Fatal("new session changed key identity", err)
	}
	cases := []struct {
		tenant, project uuid.UUID
		who             auth.Identity
		key             string
	}{
		{uuid.New(), project, who, key}, {tenant, uuid.New(), who, key}, {tenant, project, auth.Identity{UserID: uuid.New(), SessionID: who.SessionID}, key}, {tenant, project, who, key + "2"},
	}
	for _, tc := range cases {
		got, err := PerspectiveLinesSubmissionRun(tc.tenant, tc.project, tc.who, tc.key)
		if err != nil || got == first {
			t.Fatal("scope/key collision", err)
		}
	}
	if _, err := PerspectiveLinesSubmissionRun(tenant, project, who, ""); err == nil {
		t.Fatal("empty key accepted")
	}
}
func TestPerspectiveLinesPrepareSubmissionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (PerspectiveLinesPreparation{}).PrepareSubmission(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, "", PerspectiveLinesSubmissionIntent{}); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestPerspectiveLinesSubmissionRejectsInvalidBudgetBeforeDatabase(t *testing.T) {
	for _, budget := range []time.Duration{-time.Second, 6 * time.Minute} {
		_, err := (PerspectiveLinesPreparation{SubmissionTimeout: budget}).SubmitProjectIntent(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, "", PerspectiveLinesSubmissionIntent{})
		if err == nil {
			t.Fatal("invalid budget accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (PerspectiveLinesPreparation{}).SubmitProjectIntent(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, "", PerspectiveLinesSubmissionIntent{}); err != context.Canceled {
		t.Fatal(err)
	}
}
