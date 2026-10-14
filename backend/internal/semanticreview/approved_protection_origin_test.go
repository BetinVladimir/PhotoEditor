package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type protectionOriginQuery struct {
	ready bool
	err   error
	calls int
	args  []any
}
type protectionOriginRow struct{ query *protectionOriginQuery }

func (r protectionOriginRow) Scan(dest ...any) error {
	if r.query.err != nil {
		return r.query.err
	}
	*dest[0].(*bool) = r.query.ready
	return nil
}
func (q *protectionOriginQuery) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	q.calls++
	q.args = args
	return protectionOriginRow{q}
}
func TestProtectionOriginGuardChecksTypedBytesBeforeSQL(t *testing.T) {
	prepared, targets, request := decisionProofKindFixture(t, "local_exposure")
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	json.Unmarshal(raw, &images)
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	mask := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1, Protected: true}
	body, _ := canonical.Marshal(mask)
	sum := sha256.Sum256(body)
	document := PlanDocument{Source: PlanSource{Contract: "MaskRef", Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, Bytes: int64(len(body)), Bucket: "fixture", Key: "root", Version: "v1", MediaType: "application/json"}, Body: body}
	tenant, intent, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	q := &protectionOriginQuery{ready: true}
	invoke := func(ctx context.Context, doc PlanDocument) (v2input.Ref, error) {
		return VerifyApprovedProtectionOrigin(ctx, q, tenant, intent, actor, session, prepared, targets, request, request.TargetDecisions[0].TargetID, doc)
	}
	found, err := invoke(context.Background(), document)
	if err != nil || found.ArtifactID != image.Pixels.ID || q.calls != 1 || q.args[10] != document.Source.Ref.ArtifactID || q.args[12] != image.Pixels.ID || q.args[14] != string(body) {
		t.Fatal("origin guard scope", err)
	}
	modified := document
	modified.Body = append(append([]byte(nil), body...), ' ')
	if _, err := invoke(context.Background(), modified); err == nil || q.calls != 1 {
		t.Fatal("changed root reached SQL")
	}
	mask.Protected = false
	modified.Body, _ = canonical.Marshal(mask)
	sum = sha256.Sum256(modified.Body)
	modified.Source.Ref.SHA256 = hex.EncodeToString(sum[:])
	modified.Source.Bytes = int64(len(modified.Body))
	if _, err := invoke(context.Background(), modified); err == nil || q.calls != 1 {
		t.Fatal("unprotected root reached SQL")
	}
	q.ready = false
	if found, err := invoke(context.Background(), document); !errors.Is(err, ErrConflict) || found.ArtifactID != uuid.Nil {
		t.Fatal("denied origin accepted")
	}
	q.err = pgx.ErrNoRows
	if found, err := invoke(context.Background(), document); !errors.Is(err, pgx.ErrNoRows) || found.ArtifactID != uuid.Nil {
		t.Fatal("SQL error accepted")
	}
	before := q.calls
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := invoke(ctx, document); !errors.Is(err, context.Canceled) || q.calls != before {
		t.Fatal("cancel reached SQL")
	}
}
