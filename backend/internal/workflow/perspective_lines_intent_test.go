package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPerspectiveLinesIntentPreservesAuthAndOriginalBytes(t *testing.T) {
	scope, source, icc := perspectiveLinesFixture(t, true)
	plan, err := CompilePerspectiveLinesPlan(scope, source, icc)
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	snapshot := uuid.New()
	raw, err := CompilePerspectiveLinesIntent(plan, snapshot, who)
	if err != nil {
		t.Fatal(err)
	}
	again, err := CompilePerspectiveLinesIntent(plan, snapshot, who)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("unstable intent", err)
	}
	var body perspectiveLinesIntent
	if json.Unmarshal(raw, &body) != nil || body.Actor != who.UserID || body.Session != who.SessionID || body.Snapshot != snapshot || body.Base != scope.Base || body.Task != plan.Task() || body.Run != scope.Run || body.Tenant != scope.Tenant || body.Project != scope.Project || body.WorkingICC != icc || body.SourceJCS != string(source) || body.ParamsJCS != string(plan.Params()) || body.ProfileJCS != string(plan.Profile()) {
		t.Fatal("lost intent binding")
	}
	seed := uuid.NewSHA1(scope.Run, append([]byte("V29.PerspectiveLines:"), []byte(body.SeedJCS)...))
	if seed != plan.Task() {
		t.Fatal("SQL task identity seed mismatch")
	}
}
func TestPerspectiveLinesIntentRejectsMissingPrincipalSnapshotAndPlan(t *testing.T) {
	scope, source, icc := perspectiveLinesFixture(t, false)
	plan, err := CompilePerspectiveLinesPlan(scope, source, icc)
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	for _, principal := range []auth.Identity{{}, {UserID: who.UserID}, {SessionID: who.SessionID}} {
		if _, err := CompilePerspectiveLinesIntent(plan, uuid.New(), principal); err == nil {
			t.Fatal("missing principal accepted")
		}
	}
	if _, err := CompilePerspectiveLinesIntent(plan, uuid.Nil, who); err == nil {
		t.Fatal("missing snapshot accepted")
	}
	if _, err := CompilePerspectiveLinesIntent(PerspectiveLinesPlan{}, uuid.New(), who); err == nil {
		t.Fatal("uncompiled plan accepted")
	}
	plan.params = []byte(`{}`)
	if _, err := CompilePerspectiveLinesIntent(plan, uuid.New(), who); err == nil {
		t.Fatal("mutated plan accepted")
	}
}
func TestPerspectiveLinesIntentSaveCancelledBeforeDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (PerspectiveLinesIntentStore{}).Save(ctx, PerspectiveLinesPlan{}, uuid.Nil, auth.Identity{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := (PerspectiveLinesIntentStore{}).Save(context.Background(), PerspectiveLinesPlan{}, uuid.New(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
}
