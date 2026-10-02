package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func TestBeginDecodeManifestAndBarrier(t *testing.T) {
	q := &expansionQueries{row: expansionRow{values: []any{true}}}
	repo := IngestPlanRepository{DB: q}
	tenant, run := uuid.New(), uuid.New()
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	plan := IngestDecodePlan{Ready: true, Tasks: []IngestDecodeTask{{Task: PreparedTask{NodeKey: "asset.decode/0000000000000008", Operation: "asset.decode"}, Metadata: ref}}, Failures: []IngestItemFailure{{Ordinal: 2}}}
	ok, err := repo.BeginDecode(context.Background(), tenant, run, plan)
	if err != nil || !ok {
		t.Fatal(err)
	}
	var wire []map[string]any
	if json.Unmarshal(q.args[2].([]byte), &wire) != nil || len(wire) != 1 || wire[0]["artifact_id"] != ref.ID.String() || wire[0]["ordinal"] != float64(8) {
		t.Fatal("invalid manifest")
	}
	q.args = nil
	plan.Ready = false
	if _, err = repo.BeginDecode(context.Background(), tenant, run, plan); err == nil || q.args != nil {
		t.Fatal("waiting phase persisted")
	}
	plan.Ready = true
	plan.Tasks = nil
	if ok, err = repo.BeginDecode(context.Background(), tenant, run, plan); err != nil || !ok || string(q.args[2].([]byte)) != "[]" {
		t.Fatal("all failed manifest invalid", err)
	}
}
