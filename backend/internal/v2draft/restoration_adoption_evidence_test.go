package v2draft

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestRestorationEvidenceRecorderRejectsChangedLocator(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	object := objectstore.Object{Key: "tenants/object", VersionID: "v1", SHA256: ref.SHA256,
		Bytes: 100, ContentType: "application/json"}
	docs := &restorationDocumentCatalogFixture{objects: map[string]objectstore.Object{"image": object},
		reads: map[string]int{}}
	arrays := &restorationArrayCatalogFixture{objects: map[string]objectstore.Object{},
		reads: map[string]int{}}
	recorder := newRestorationLocatorRecorder(docs, arrays)
	scope := RestorationArrayScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New(),
		Root: ref.ArtifactID, RootSHA: ref.SHA256}
	if _, err := recorder.ResolveRestorationDocument(context.Background(), scope, "image", ref); err != nil {
		t.Fatal(err)
	}
	docs.change = false
	if _, err := recorder.ResolveRestorationDocument(context.Background(), scope, "image", ref); err != nil {
		t.Fatal("stable locator refused", err)
	}
	if recorder.counts["document/image"] != 2 || recorder.records["document/image"].Version != "v1" {
		t.Fatal("verified version not retained")
	}
	docs.reads = map[string]int{"image": 1}
	docs.change = true
	if _, err := recorder.ResolveRestorationDocument(context.Background(), scope, "image", ref); err == nil {
		t.Fatal("third changed locator accepted")
	}
}
