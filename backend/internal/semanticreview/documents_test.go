package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func documentSources() (PlanSources, []byte) {
	body := []byte(`{"schema_version":1}`)
	sum := sha256.Sum256(body)
	sources := PlanSources{RunID: uuid.New(), TaskID: uuid.New(), ResultID: uuid.New(), Revision: 1}
	for _, contract := range []string{"SemanticPlan", "Params", "InputSnapshot", "EditIntent", "Calibration", "TransferCandidate"} {
		sources.Objects = append(sources.Objects, PlanSource{Contract: contract, Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, Bytes: int64(len(body)), Bucket: "bucket", Key: contract, Version: "v1", MediaType: "application/json"})
	}
	return sources, body
}

func TestReadPlanDocumentsOwnsBytesAndMetadataAndChecksBudgetBeforeIO(t *testing.T) {
	sources, body := documentSources()
	original := append([]PlanSource(nil), sources.Objects...)
	calls := 0
	read := func(_ context.Context, source PlanSource) ([]byte, error) {
		calls++
		if source != original[calls-1] {
			t.Fatal("metadata alias")
		}
		sources.Objects[len(sources.Objects)-1].Version = "changed"
		return body, nil
	}
	if _, err := ReadPlanDocuments(context.Background(), sources, read, 1); err == nil || calls != 0 {
		t.Fatal("budget admission")
	}
	docs, err := ReadPlanDocuments(context.Background(), sources, read, 1024)
	if err != nil || calls != 6 {
		t.Fatal(err, calls)
	}
	body[0] = 'x'
	if docs[0].Body[0] != '{' || docs[5].Source.Version != "v1" {
		t.Fatal("ownership")
	}
}

func TestReadPlanDocumentsRejectsAlteredBytesAndCancellation(t *testing.T) {
	sources, body := documentSources()
	for _, bad := range [][]byte{append(append([]byte(nil), body...), byte(' ')), []byte(`{"schema_version":2}`)} {
		if _, err := ReadPlanDocuments(context.Background(), sources, func(context.Context, PlanSource) ([]byte, error) { return bad, nil }, 1024); err == nil {
			t.Fatal("altered object accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	if _, err := ReadPlanDocuments(ctx, sources, func(context.Context, PlanSource) ([]byte, error) { calls++; return body, nil }, 1024); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal(err, calls)
	}
	failure := errors.New("storage unavailable")
	if _, err := ReadPlanDocuments(context.Background(), sources, func(context.Context, PlanSource) ([]byte, error) { return nil, failure }, 1024); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestSourceFenceIncludesPhysicalVersionAndProducerGeneration(t *testing.T) {
	before, _ := documentSources()
	after := before
	after.Objects = append([]PlanSource(nil), before.Objects...)
	after.Objects[0], after.Objects[5] = after.Objects[5], after.Objects[0]
	if !EqualPlanSources(before, after) {
		t.Fatal("SQL order should not matter")
	}
	after.Objects[0].Version = "v2"
	if EqualPlanSources(before, after) {
		t.Fatal("object version change")
	}
	after = before
	after.ResultID = uuid.New()
	if EqualPlanSources(before, after) {
		t.Fatal("producer result change")
	}
	after = before
	after.Revision++
	if EqualPlanSources(before, after) {
		t.Fatal("review revision change")
	}
}

func TestCurrentPlanDocumentsReauthorizeAfterStorageAndOwnOriginalInventory(t *testing.T) {
	for _, drift := range []bool{false, true} {
		sources, body := documentSources()
		loads := 0
		load := func(context.Context) (PlanSources, error) { loads++; return sources, nil }
		read := func(context.Context, PlanSource) ([]byte, error) {
			if drift {
				sources.Objects[0].Version = "v2"
			}
			return body, nil
		}
		owned, documents, err := ReadCurrentPlanDocuments(context.Background(), load, read, 1024)
		if loads != 2 {
			t.Fatal("missing authorization re-read")
		}
		if drift {
			if !errors.Is(err, ErrConflict) || documents != nil {
				t.Fatal("source drift accepted")
			}
		} else if err != nil || len(documents) != 6 || owned.Objects[0].Version != "v1" {
			t.Fatal(err)
		}
	}
	sources, body := documentSources()
	loads := 0
	revoked := errors.New("authorization revoked")
	load := func(context.Context) (PlanSources, error) {
		loads++
		if loads == 2 {
			return PlanSources{}, revoked
		}
		return sources, nil
	}
	if _, documents, err := ReadCurrentPlanDocuments(context.Background(), load, func(context.Context, PlanSource) ([]byte, error) { return body, nil }, 1024); !errors.Is(err, revoked) || documents != nil {
		t.Fatal(err)
	}
}
