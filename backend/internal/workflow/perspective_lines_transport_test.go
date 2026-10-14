package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type linesTransportDocuments struct {
	calls  int
	alias  uuid.UUID
	scopes []artifactjson.Scope
}

func (d *linesTransportDocuments) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	d.calls++
	d.scopes = append(d.scopes, scope)
	id := uuid.NewSHA1(scope.Owner, []byte(scope.Purpose))
	if d.alias != uuid.Nil {
		id = d.alias
	}
	return artifactjson.Ref{ID: id, SHA256: renderDerivativeBytesSHA(body)}, nil
}
func TestPerspectiveLinesTransportPublisherBuildsAndReplaysExactClosure(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, true)
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	docs := &linesTransportDocuments{}
	publisher, err := NewPerspectiveLinesTransportPublisher(plan, params, docs)
	if err != nil {
		t.Fatal(err)
	}
	first, err := BuildPerspectiveLinesClosure(context.Background(), plan, params, publisher)
	if err != nil {
		t.Fatal(err)
	}
	again, err := BuildPerspectiveLinesClosure(context.Background(), plan, params, publisher)
	if err != nil || first.Built().Manifest != again.Built().Manifest {
		t.Fatal("replay changed", err)
	}
	if docs.calls != 4 || docs.scopes[0].Purpose != "v2_input_chunk_0" || docs.scopes[1].Purpose != "v2_input_manifest" {
		t.Fatal("wrong publication scopes")
	}
	for _, s := range docs.scopes {
		if s.Tenant != scope.Tenant || s.Run != scope.Run || s.Owner != plan.Task() || s.AssemblyLease != uuid.Nil {
			t.Fatal("foreign scope")
		}
	}
	for _, raw := range first.Documents() {
		var value map[string]json.RawMessage
		if json.Unmarshal(raw, &value) != nil {
			t.Fatal("invalid control document")
		}
	}
}
func TestPerspectiveLinesTransportDocumentOwnsBytesAndProof(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, false)
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	entries, err := plan.Entries(params)
	if err != nil {
		t.Fatal(err)
	}
	body, err := canonical.Marshal(v2input.Chunk{SchemaVersion: 1, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(body)
	doc, err := CompilePerspectiveLinesTransportDocument(plan, params, 0, "InputClosureChunk", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	body[0] = 'x'
	exposed := doc.Bytes()
	exposed[0] = 'x'
	if !bytes.Equal(doc.Bytes(), original) {
		t.Fatal("mutable bytes escaped")
	}
	proof, err := doc.Proof()
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Body string `json:"body_jcs"`
		SHA  string `json:"sha256"`
	}
	if json.Unmarshal(proof, &wire) != nil || wire.Body != string(original) || wire.SHA != renderDerivativeBytesSHA(original) {
		t.Fatal("original-byte proof lost")
	}
	if _, err := CompilePerspectiveLinesTransportDocument(plan, params, 1, "InputClosureChunk", original, nil); err == nil {
		t.Fatal("wrong ordinal accepted")
	}
	if _, err := CompilePerspectiveLinesTransportDocument(plan, params, 0, "InputClosureChunk", append(original, ' '), nil); err == nil {
		t.Fatal("noncanonical body accepted")
	}
}
func TestPerspectiveLinesTransportRejectsForeignTaskManifestBeforeChunkAndAliases(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, false)
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	docs := &linesTransportDocuments{}
	publisher, err := NewPerspectiveLinesTransportPublisher(plan, params, docs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(context.Background(), uuid.New(), 0, "InputClosureChunk", []byte(`{}`)); err == nil {
		t.Fatal("foreign task accepted")
	}
	if _, err := publisher.Publish(context.Background(), plan.Task(), 1, "InputClosureManifest", []byte(`{}`)); err == nil {
		t.Fatal("unbound manifest accepted")
	}
	if docs.calls != 0 {
		t.Fatal("IO before validation")
	}
	docs.alias = params.ID
	if _, err := BuildPerspectiveLinesClosure(context.Background(), plan, params, publisher); err == nil {
		t.Fatal("Params alias accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := publisher.Publish(ctx, plan.Task(), 0, "InputClosureChunk", []byte(`{}`)); err != context.Canceled {
		t.Fatal(err)
	}
}
