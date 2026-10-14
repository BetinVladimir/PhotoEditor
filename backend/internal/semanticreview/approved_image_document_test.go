package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

type imageDocumentQuery struct {
	metadata protectionDocumentQuery
	args     []any
}

func (q *imageDocumentQuery) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	q.args = args
	return q.metadata.QueryRow(ctx, query, args...)
}
func TestImageDocumentReaderDerivesReferenceAndDeniesBeforeIO(t *testing.T) {
	prepared, targets, request := decisionProofKindFixture(t, "local_exposure")
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil || len(edits) != 1 {
		t.Fatal(err)
	}
	tenant, intent, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, reference := range []bool{false, true} {
		pin := edits[0].Source.Source
		if reference {
			pin = prepared.Intent.Reference.Source
		}
		source := PlanSource{Contract: "WorkingImage", Ref: pin, Bytes: 8388609, Bucket: "fixture", Key: "image", Version: "v1", MediaType: "application/json"}
		q := &imageDocumentQuery{metadata: protectionDocumentQuery{sources: []PlanSource{source}}}
		reads := 0
		reader := func(context.Context, PlanSource) ([]byte, error) { reads++; return nil, nil }
		document, err := ReadApprovedImageDocument(context.Background(), q, tenant, intent, actor, session, prepared, targets, request, edits[0].TargetID, reference, reader)
		if err == nil || document.Body != nil || reads != 0 || q.metadata.calls != 1 {
			t.Fatal("oversized image read")
		}
		if q.args[10] != reference || q.args[11] != pin.ArtifactID || q.args[12] != pin.SHA256 || q.args[8] != edits[0].Candidate.ArtifactID {
			t.Fatal("image scope projection changed")
		}
	}
}
