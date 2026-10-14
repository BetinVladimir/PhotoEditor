package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

type candidateDocumentQuery struct {
	metadata protectionDocumentQuery
	args     []any
}

func (q *candidateDocumentQuery) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	q.args = args
	return q.metadata.QueryRow(ctx, query, args...)
}
func TestCandidateDocumentReaderDerivesChildAndDeniesBeforeIO(t *testing.T) {
	prepared, targets, request := decisionProofKindFixture(t, "local_exposure")
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil || len(edits) != 1 {
		t.Fatal(err)
	}
	tenant, intent, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, transform := range []bool{false, true} {
		pin, contract, kind := edits[0].Mask, "MaskRef", "mask"
		if transform {
			pin, contract, kind = edits[0].Transform, "ResolvedTransform", "transform"
		}
		source := PlanSource{Contract: contract, Ref: pin, Bytes: 8388609, Bucket: "fixture", Key: "image", Version: "v1", MediaType: "application/json"}
		q := &candidateDocumentQuery{metadata: protectionDocumentQuery{sources: []PlanSource{source}}}
		reads := 0
		reader := func(context.Context, PlanSource) ([]byte, error) { reads++; return nil, nil }
		document, err := ReadApprovedCandidateDocument(context.Background(), q, tenant, intent, actor, session, prepared, targets, request, edits[0].TargetID, transform, reader)
		if err == nil || document.Body != nil || reads != 0 || q.metadata.calls != 1 {
			t.Fatal("oversized candidate child read")
		}
		if q.args[11] != kind || len(q.args[10].(string)) < 2 || q.args[8] != edits[0].Candidate.ArtifactID {
			t.Fatal("candidate child scope projection changed")
		}
	}
}
