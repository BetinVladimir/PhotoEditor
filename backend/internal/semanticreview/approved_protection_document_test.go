package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type protectionDocumentQuery struct {
	sources []PlanSource
	calls   int
}
type protectionDocumentRow struct{ source PlanSource }

func (q *protectionDocumentQuery) QueryRow(context.Context, string, ...any) pgx.Row {
	index := q.calls
	q.calls++
	if index >= len(q.sources) {
		index = len(q.sources) - 1
	}
	return protectionDocumentRow{q.sources[index]}
}
func (r protectionDocumentRow) Scan(dest ...any) error {
	s := r.source
	*dest[0].(*string) = s.Contract
	*dest[1].(*uuid.UUID) = s.Ref.ArtifactID
	*dest[2].(*string) = s.Ref.SHA256
	*dest[3].(*int64) = s.Bytes
	*dest[4].(*string) = s.Bucket
	*dest[5].(*string) = s.Key
	*dest[6].(*string) = s.Version
	*dest[7].(*string) = s.MediaType
	return nil
}
func TestProtectionDocumentReauthorizesAndOwnsPinnedBytes(t *testing.T) {
	prepared, targets, request := decisionProofKindFixture(t, "local_exposure")
	raw := []byte("{}")
	sum := sha256.Sum256(raw)
	pin := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	source := PlanSource{Contract: "MaskRef", Ref: pin, Bytes: 2, Bucket: "fixture", Key: "root", Version: "v1", MediaType: "application/json"}
	tenant, intent, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	invoke := func(q *protectionDocumentQuery, reader PlanObjectReader) (PlanDocument, error) {
		return ReadApprovedProtectionDocument(context.Background(), q, tenant, intent, actor, session, prepared, targets, request, request.TargetDecisions[0].TargetID, pin, reader)
	}
	q := &protectionDocumentQuery{sources: []PlanSource{source}}
	document, err := invoke(q, func(context.Context, PlanSource) ([]byte, error) { return raw, nil })
	if err != nil || q.calls != 2 || string(document.Body) != "{}" {
		t.Fatal("scoped document read", err)
	}
	raw[0] = '!'
	if string(document.Body) != "{}" {
		t.Fatal("borrowed object bytes")
	}
	q = &protectionDocumentQuery{sources: []PlanSource{source}}
	if document, err := invoke(q, func(context.Context, PlanSource) ([]byte, error) { return raw, nil }); err == nil || document.Body != nil || q.calls != 1 {
		t.Fatal("altered bytes accepted")
	}
	changed := source
	changed.Version = "v2"
	q = &protectionDocumentQuery{sources: []PlanSource{source, changed}}
	if document, err := invoke(q, func(context.Context, PlanSource) ([]byte, error) { return []byte("{}"), nil }); err == nil || document.Body != nil || q.calls != 2 {
		t.Fatal("scope changed after IO accepted")
	}
	oversized := source
	oversized.Bytes = 8388609
	q = &protectionDocumentQuery{sources: []PlanSource{oversized}}
	reads := 0
	if _, err := invoke(q, func(context.Context, PlanSource) ([]byte, error) { reads++; return nil, nil }); err == nil || reads != 0 {
		t.Fatal("oversized root read")
	}
}
