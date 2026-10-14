package interchange

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type importAdoptionCatalogStub struct {
	root     v2input.Ref
	pins     map[uuid.UUID]objectstore.Object
	lookup   int
	rotateAt int
	current  bool
}

func (s *importAdoptionCatalogStub) RootImportAdoption(_ context.Context, _ ImportAdoptionClaim, _ string) (v2input.Ref, objectstore.Object, error) {
	return s.root, s.pins[s.root.ArtifactID], nil
}
func (s *importAdoptionCatalogStub) LookupImportOutput(_ context.Context, _ ImportAdoptionClaim,
	ref v2input.Ref, _ string, _ string) (objectstore.Object, error) {
	s.lookup++
	object := s.pins[ref.ArtifactID]
	if s.rotateAt != 0 && s.lookup >= s.rotateAt {
		object.VersionID = "rotated"
	}
	return object, nil
}
func (s *importAdoptionCatalogStub) CurrentImportAdoptionClaim(context.Context, ImportAdoptionClaim) (bool, error) {
	return s.current, nil
}

type importAdoptionObjectsStub struct{ bodies map[string][]byte }

func (s importAdoptionObjectsStub) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.bodies[object.Key])), nil
}

func importAdoptionResultFixture(t *testing.T) (ImportAdoptionResultReader, ImportAdoptionClaim, *importAdoptionCatalogStub) {
	t.Helper()
	bodies := map[string][]byte{}
	pins := map[uuid.UUID]objectstore.Object{}
	publish := func(value any) v2input.Ref {
		t.Helper()
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}
		key := "test/" + ref.ArtifactID.String()
		pins[ref.ArtifactID] = objectstore.Object{Key: key, VersionID: "version-1",
			SHA256: ref.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}
		bodies[key] = raw
		return ref
	}
	version := uuid.New()
	shard := publish(exportMappingShard{Schema: 1, Entries: []exportMappingEntry{{
		ID: uuid.New(), External: "camera-one", Version: &version,
		Fields: []exportMappingField{{Name: "rating", Status: "exact"}},
	}}})
	report := publish(exportMappingReport{Schema: 1, Count: 1, Shards: []v2input.Ref{shard}})
	imported := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("source"))}
	root := publish(exportResultDocument{Schema: 1, Report: report, Bundle: nil,
		Drafts: []v2input.Ref{shard}, Imported: []v2input.Ref{imported}})
	workflow := uuid.New()
	claim := ImportAdoptionClaim{Tenant: uuid.New(), Workflow: workflow, Run: uuid.New(),
		Task:   uuid.NewSHA1(workflow, []byte("v2.interchange.import:0")),
		Result: uuid.New(), Attempt: 1, Fence: 1, Token: uuid.New()}
	catalog := &importAdoptionCatalogStub{root: root, pins: pins, current: true}
	reader := ImportAdoptionResultReader{Catalog: catalog,
		Objects: importAdoptionObjectsStub{bodies: bodies}, Bucket: "photos"}
	return reader, claim, catalog
}

func TestImportAdoptionResultReaderChecksPhysicalInventory(t *testing.T) {
	reader, claim, _ := importAdoptionResultFixture(t)
	got, err := reader.Read(context.Background(), claim)
	if err != nil || len(got.Shards) != 1 || len(got.Imported) != 1 ||
		got.Root.Ref.ArtifactID == uuid.Nil || got.Report.Ref.ArtifactID == uuid.Nil {
		t.Fatal("valid physical import result refused", err)
	}
}

func TestImportAdoptionResultReaderRejectsObjectVersionRotation(t *testing.T) {
	reader, claim, catalog := importAdoptionResultFixture(t)
	catalog.rotateAt = 2
	if _, err := reader.Read(context.Background(), claim); err == nil {
		t.Fatal("rotated root version accepted")
	}
}

func TestImportAdoptionResultReaderRejectsMissingClaim(t *testing.T) {
	reader, claim, catalog := importAdoptionResultFixture(t)
	catalog.current = false
	if _, err := reader.Read(context.Background(), claim); err == nil || catalog.lookup != 0 {
		t.Fatal("stale claim reached object lookup")
	}
}
