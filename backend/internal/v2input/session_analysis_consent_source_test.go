package v2input

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestCompileSessionConsentSnapshotRequiresSelectedFaceAndGrant(t *testing.T) {
	run := uuid.New()
	image := Ref{uuid.New(), strings.Repeat("a", 64)}
	version := uuid.New()
	snapshot, _, err := CompileSessionInputSnapshot(run, []SessionSnapshotItem{{
		Ordinal: 0, Binding: SessionVersionBinding{AssetVersionID: version,
			Source: image, CanvasHash: strings.Repeat("b", 64)},
		WorkingImage: image, Recipe: Ref{uuid.New(), strings.Repeat("c", 64)}}})
	if err != nil {
		t.Fatal(err)
	}
	person, consent := uuid.New(), uuid.New()
	doc := struct {
		SchemaVersion  int                    `json:"schema_version"`
		Grants         []sessionConsentGrant  `json:"grants"`
		ConfirmedFaces []sessionConfirmedFace `json:"confirmed_faces"`
	}{1, []sessionConsentGrant{{ConsentID: consent, Epoch: "2",
		PersonID: &person, Purpose: "identity_matching"}},
		[]sessionConfirmedFace{{AssetVersionID: version, FaceID: uuid.New(),
			PersonID: person, ConsentID: consent, Epoch: "2"}}}
	raw, _ := json.Marshal(doc)
	body, err := CompileSessionConsentSnapshot(raw, snapshot)
	if err != nil || validateSessionConsentSnapshot(body) != nil {
		t.Fatalf("valid consent snapshot: %v", err)
	}
	canonicalBody, _ := canonical.Bytes(raw)
	if string(body) != string(canonicalBody) {
		t.Fatal("body was not canonical")
	}
	doc.ConfirmedFaces[0].AssetVersionID = uuid.New()
	raw, _ = json.Marshal(doc)
	if _, err := CompileSessionConsentSnapshot(raw, snapshot); err == nil {
		t.Fatal("accepted a face outside frozen inventory")
	}
	doc.ConfirmedFaces[0].AssetVersionID = version
	doc.Grants = []sessionConsentGrant{}
	raw, _ = json.Marshal(doc)
	if _, err := CompileSessionConsentSnapshot(raw, snapshot); err == nil {
		t.Fatal("accepted a face without active pinned grant")
	}
}
