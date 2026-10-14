package interchange

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type exportOutputReaderStub struct {
	docs                    map[uuid.UUID]ExportResultDocumentBytes
	calls                   map[uuid.UUID]int
	opens                   int
	change, revoke, corrupt bool
}

func (s *exportOutputReaderStub) LookupExportOutput(_ context.Context, e v2input.Entry) (objectstore.Object, error) {
	doc, ok := s.docs[e.Ref.ArtifactID]
	if !ok || doc.Ref != e.Ref {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	s.calls[e.Ref.ArtifactID]++
	if s.revoke && s.calls[e.Ref.ArtifactID] > 1 {
		return objectstore.Object{}, errors.New("authority revoked")
	}
	version := "version-1"
	if s.change && s.calls[e.Ref.ArtifactID] > 1 {
		version = "version-2"
	}
	return objectstore.Object{Key: e.Ref.ArtifactID.String(), VersionID: version, ContentType: "application/json", Bytes: int64(len(doc.Body)), SHA256: e.Ref.SHA256}, nil
}
func (s *exportOutputReaderStub) Open(_ context.Context, o objectstore.Object) (io.ReadCloser, error) {
	s.opens++
	id, err := uuid.Parse(o.Key)
	if err != nil {
		return nil, err
	}
	body := append([]byte(nil), s.docs[id].Body...)
	if s.corrupt {
		body[0] = 'x'
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}
func exportOutputReaderFixture(t *testing.T) (CreateIntent, FrozenExportDocuments, v2input.Ref, *exportOutputReaderStub) {
	t.Helper()
	intent, compiled, snapshot, raw := exportFreezeFixture(t)
	frozen, err := FreezeExportDocuments(context.Background(), intent, compiled, snapshot, raw)
	if err != nil {
		t.Fatal(err)
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil {
		t.Fatal(err)
	}
	item := plan.Entries[0]
	version := item.Binding.AssetVersionID
	shard := exportResultJSON(t, exportMappingShard{Schema: 1, Entries: []exportMappingEntry{{ID: item.ID, External: item.External, Version: &version, Fields: sidecarFieldsForTest(item)}}})
	report := exportResultJSON(t, exportMappingReport{Schema: 1, Count: 1, Shards: []v2input.Ref{shard.Ref}, Unsupported: 1})
	bundle := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("zip"))}
	root := exportResultJSON(t, exportResultDocument{Schema: 1, Report: report.Ref, Bundle: &bundle, Drafts: []v2input.Ref{}, Imported: []v2input.Ref{}})
	stub := &exportOutputReaderStub{docs: map[uuid.UUID]ExportResultDocumentBytes{}, calls: map[uuid.UUID]int{}}
	for _, doc := range []ExportResultDocumentBytes{root, report, shard} {
		stub.docs[doc.Ref.ArtifactID] = doc
	}
	return intent, frozen, root.Ref, stub
}
func TestExportResultReaderReadsAndRechecksAllJSONPins(t *testing.T) {
	intent, frozen, root, stub := exportOutputReaderFixture(t)
	receipt, err := (ExportResultReader{Catalog: stub, Objects: stub}).Read(context.Background(), intent, frozen, root)
	if err != nil || receipt.Root() != root || stub.opens != 3 {
		t.Fatal("read", err)
	}
	for _, count := range stub.calls {
		if count != 2 {
			t.Fatal("output not rechecked")
		}
	}
}
func TestExportResultReaderRejectsRevocationVersionChangeAndCorruption(t *testing.T) {
	for _, kind := range []string{"revoke", "change", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			intent, frozen, root, stub := exportOutputReaderFixture(t)
			stub.revoke = kind == "revoke"
			stub.change = kind == "change"
			stub.corrupt = kind == "corrupt"
			if _, err := (ExportResultReader{Catalog: stub, Objects: stub}).Read(context.Background(), intent, frozen, root); err == nil {
				t.Fatal("invalid result read")
			}
		})
	}
}
func TestExportResultReaderRejectsMissingDependenciesAndCancelledContextBeforeIO(t *testing.T) {
	intent, frozen, root, stub := exportOutputReaderFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, reader := range []ExportResultReader{{}, {Catalog: stub}, {Objects: stub}} {
		if _, err := reader.Read(context.Background(), intent, frozen, root); err == nil {
			t.Fatal("missing dependency")
		}
	}
	if _, err := (ExportResultReader{Catalog: stub, Objects: stub}).Read(ctx, intent, frozen, root); err == nil || stub.opens != 0 {
		t.Fatal("cancelled read")
	}
}
