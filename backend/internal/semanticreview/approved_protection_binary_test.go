package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type protectionBinaryQuery struct {
	origin   protectionOriginQuery
	metadata protectionDocumentQuery
}

func (q *protectionBinaryQuery) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, "protection_origin(") {
		return q.origin.QueryRow(ctx, query, args...)
	}
	return q.metadata.QueryRow(ctx, query, args...)
}
func TestProtectionBinaryStreamsClosesAndReauthorizes(t *testing.T) {
	prepared, targets, request := decisionProofKindFixture(t, "local_exposure")
	body, _ := base64.StdEncoding.DecodeString("k05VTVBZAQB2AHsnZGVzY3InOiAnPGY0JywgJ2ZvcnRyYW5fb3JkZXInOiBGYWxzZSwgJ3NoYXBlJzogKDIsIDMpLCB9ICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgIAoAAAAAAAAAPwAAgD8AAIA/AACAPgAAAAA=")
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
	sum := sha256.Sum256(body)
	pin := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	image.Canvas.Width = 3
	image.Canvas.Height = 2
	image.Pixels.ID = pin.ArtifactID
	image.Pixels.SHA256 = pin.SHA256
	mask := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1, Protected: true}
	rootBody, _ := canonical.Marshal(mask)
	sum = sha256.Sum256(rootBody)
	root := PlanDocument{Source: PlanSource{Contract: "MaskRef", Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, Bytes: int64(len(rootBody)), Bucket: "fixture", Key: "root", Version: "v1", MediaType: "application/json"}, Body: rootBody}
	source := PlanSource{Contract: "NpyArray", Ref: pin, Bytes: int64(len(body)), Bucket: "fixture", Key: "pixels", Version: "v1", MediaType: "application/x-npy"}
	tenant, intent, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	invoke := func(q *protectionBinaryQuery, store *planStoreFixture) (PlanSource, error) {
		return VerifyApprovedProtectionBinary(context.Background(), q, store, "fixture", tenant, intent, actor, session, prepared, targets, request, request.TargetDecisions[0].TargetID, root)
	}
	q := &protectionBinaryQuery{origin: protectionOriginQuery{ready: true}, metadata: protectionDocumentQuery{sources: []PlanSource{source}}}
	store := &planStoreFixture{Body: body}
	found, err := invoke(q, store)
	if err != nil || found != source || len(store.Opened) != 1 || store.Closed != 1 || q.metadata.calls != 2 || store.Opened[0].VersionID != "v1" {
		t.Fatal("stream verification", err)
	}
	q = &protectionBinaryQuery{origin: protectionOriginQuery{ready: false}, metadata: protectionDocumentQuery{sources: []PlanSource{source}}}
	store = &planStoreFixture{Body: body}
	if _, err := invoke(q, store); err == nil || len(store.Opened) != 0 {
		t.Fatal("unproven binary opened")
	}
	altered := append([]byte(nil), body...)
	altered[len(altered)-1] ^= 1
	q = &protectionBinaryQuery{origin: protectionOriginQuery{ready: true}, metadata: protectionDocumentQuery{sources: []PlanSource{source}}}
	store = &planStoreFixture{Body: altered}
	if _, err := invoke(q, store); err == nil || store.Closed != 1 || q.metadata.calls != 1 {
		t.Fatal("altered NPY accepted")
	}
	changed := source
	changed.Version = "v2"
	q = &protectionBinaryQuery{origin: protectionOriginQuery{ready: true}, metadata: protectionDocumentQuery{sources: []PlanSource{source, changed}}}
	store = &planStoreFixture{Body: body}
	if _, err := invoke(q, store); err == nil || store.Closed != 1 || q.metadata.calls != 2 {
		t.Fatal("post-IO changed scope accepted")
	}
}
