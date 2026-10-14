package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestAnalysisTransportRepositoryRejectsForeignScopeBeforeDB(t *testing.T) {
	request := analysisPlanFixture(t)
	plan, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	s := plan.Snapshot()
	body, err := canonical.Marshal(v2input.Chunk{SchemaVersion: 1, Entries: s.Inputs})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	db := &noAnalysisDraftDB{}
	repository := relightAnalysisTransportRepository{coordinator: RelightAnalysisCoordinator{DB: db, Bucket: "photos"}, prepared: PreparedRelightAnalysis{plan: plan}, body: string(body)}
	scope := artifactjson.Scope{Tenant: request.Tenant, Run: s.Run, Owner: s.Task, Purpose: "v2_input_chunk_0"}
	if _, err := repository.proof(scope, sha, int64(len(body))); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"tenant", "run", "owner", "purpose", "chunk1", "lease", "local", "donor", "hash", "size"} {
		changed := scope
		digest := sha
		size := int64(len(body))
		switch fault {
		case "tenant":
			changed.Tenant = uuid.New()
		case "run":
			changed.Run = uuid.New()
		case "owner":
			changed.Owner = uuid.New()
		case "purpose":
			changed.Purpose = "task_params"
		case "chunk1":
			changed.Purpose = "v2_input_chunk_1"
		case "lease":
			changed.AssemblyLease = uuid.New()
		case "local":
			changed.LocalPortrait = true
		case "donor":
			changed.DonorPortrait = true
		case "hash":
			digest = "invalid"
		case "size":
			size++
		}
		if _, err := repository.Reserve(context.Background(), changed, digest, size); err == nil {
			t.Fatal("accepted", fault)
		}
	}
	if db.calls != 0 {
		t.Fatal("invalid scope accessed database")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.Reserve(ctx, scope, sha, int64(len(body))); err != context.Canceled {
		t.Fatal(err)
	}
	if db.calls != 0 {
		t.Fatal("cancelled reserve accessed database")
	}
}
