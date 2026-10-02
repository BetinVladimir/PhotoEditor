package workflow

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestOriginalRepositoryBindsScopeAndCompletionFence(t *testing.T) {
	spec, _ := ingestFixture(t)
	scope := OriginalScope{uuid.New(), spec.RunID, spec.Items[0].Ordinal, spec.Items[0].Source}
	id, token := uuid.New(), uuid.New()
	var version *string
	db := &expansionQueries{row: expansionRow{values: []any{"resumed", &id, &token, version}}}
	repo := OriginalRepository{DB: db, Bucket: "bucket"}
	reservation, err := repo.Reserve(context.Background(), scope)
	if err != nil || reservation.Ready || reservation.ArtifactID != id || len(db.args) != 6 || db.args[3] != scope.Source.ID || db.args[4] != scope.Source.SHA256 {
		t.Fatal("reserve binding failed", err)
	}
	db.row = expansionRow{values: []any{true}}
	ok, err := repo.Complete(context.Background(), scope, reservation, "v1")
	if err != nil || !ok || db.args[4] != token || db.args[3] != id || db.args[5] != "v1" {
		t.Fatal("completion fence lost", err)
	}
}
func TestOriginalRepositoryRejectsInconsistentResponses(t *testing.T) {
	spec, _ := ingestFixture(t)
	scope := OriginalScope{uuid.New(), spec.RunID, spec.Items[0].Ordinal, spec.Items[0].Source}
	id, token := uuid.New(), uuid.New()
	v := "v1"
	var missing *string
	for _, test := range []struct {
		outcome string
		version *string
		valid   bool
	}{{"created", missing, true}, {"resumed", missing, true}, {"replayed", &v, true}, {"created", &v, false}, {"replayed", missing, false}, {"quota", missing, false}, {"stale", missing, false}} {
		db := &expansionQueries{row: expansionRow{values: []any{test.outcome, &id, &token, test.version}}}
		got, err := (OriginalRepository{DB: db, Bucket: "bucket"}).Reserve(context.Background(), scope)
		if (err == nil) != test.valid {
			t.Fatal("invalid response handling", test.outcome, err)
		}
		if test.valid && test.outcome == "replayed" && (!got.Ready || got.Version != v) {
			t.Fatal("ready replay lost")
		}
	}
}
