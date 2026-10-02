package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

type entryCursor struct {
	entries []Entry
	index   int
}

func (c *entryCursor) Next(context.Context) (Entry, bool, error) {
	if c.index == len(c.entries) {
		return Entry{}, false, nil
	}
	e := c.entries[c.index]
	c.index++
	return e, true, nil
}

type memoryPublisher struct {
	bodies map[uuid.UUID][]byte
	bad    bool
}

func (p *memoryPublisher) Publish(_ context.Context, _ uuid.UUID, ordinal int, contract string, body []byte) (Ref, error) {
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("%s:%d", contract, ordinal)))
	digest := sha256.Sum256(body)
	sha := hex.EncodeToString(digest[:])
	if p.bad {
		if sha[0] == '0' {
			sha = "1" + sha[1:]
		} else {
			sha = "0" + sha[1:]
		}
	}
	p.bodies[id] = append([]byte(nil), body...)
	return Ref{ArtifactID: id, SHA256: sha}, nil
}

func TestSingleEntryDigestMatchesPythonRuntime(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	task := uuid.New()
	p := &memoryPublisher{bodies: make(map[uuid.UUID][]byte)}
	built, err := BuildInputClosure(context.Background(), task, &entryCursor{entries: []Entry{
		{Ref: Ref{ArtifactID: id, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Contract: "image/tiff"},
	}}, p)
	if err != nil {
		t.Fatal(err)
	}
	const pythonDigest = "466cff0eb84a8b91731a35813d9b4c972f1899afaf55c81617ab51bb53fbe28b"
	if built.Count != 1 || built.SHA256 != pythonDigest || len(built.Chunks) != 1 {
		t.Fatalf("unexpected closure: %+v", built)
	}
	var manifest Manifest
	if err := json.Unmarshal(p.bodies[built.Manifest.ArtifactID], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.TaskID != task || manifest.EntryCount != 1 || manifest.EntriesSHA != pythonDigest || len(manifest.Chunks) != 1 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func TestChunkBoundaryAndExactManifest(t *testing.T) {
	entries := make([]Entry, 513)
	for i := range entries {
		id := uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012x", i+1))
		entries[i] = Entry{Ref: Ref{ArtifactID: id, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Contract: "image/tiff"}
	}
	p := &memoryPublisher{bodies: make(map[uuid.UUID][]byte)}
	built, err := BuildInputClosure(context.Background(), uuid.New(), &entryCursor{entries: entries}, p)
	if err != nil {
		t.Fatal(err)
	}
	if built.Count != 513 || len(built.Chunks) != 2 {
		t.Fatalf("wrong chunk boundary: %+v", built)
	}
	for i, ref := range built.Chunks {
		var chunk Chunk
		if err := json.Unmarshal(p.bodies[ref.ArtifactID], &chunk); err != nil {
			t.Fatal(err)
		}
		want := 512
		if i == 1 {
			want = 1
		}
		if chunk.SchemaVersion != 1 || len(chunk.Entries) != want {
			t.Fatalf("chunk %d has %d entries", i, len(chunk.Entries))
		}
	}
	array, err := canonical.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(array)
	if built.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("streaming digest differs from JCS array")
	}
}

func TestRejectsDuplicateArtifactAndPublisherHashMismatch(t *testing.T) {
	id := uuid.New()
	entry := Entry{Ref: Ref{ArtifactID: id, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Contract: "image/tiff"}
	if _, err := BuildInputClosure(context.Background(), uuid.New(), &entryCursor{entries: []Entry{entry, entry}},
		&memoryPublisher{bodies: make(map[uuid.UUID][]byte)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate accepted: %v", err)
	}
	if _, err := BuildInputClosure(context.Background(), uuid.New(), &entryCursor{entries: []Entry{entry}},
		&memoryPublisher{bodies: make(map[uuid.UUID][]byte), bad: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("forged publisher SHA accepted: %v", err)
	}
}

type scopedDocuments struct{ scopes []artifactjson.Scope }

func (d *scopedDocuments) Publish(_ context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	d.scopes = append(d.scopes, scope)
	digest := sha256.Sum256(body)
	return artifactjson.Ref{ID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}, nil
}

func TestScopedPublisherGivesEveryChunkStableTaskScope(t *testing.T) {
	tenant, run, task := uuid.New(), uuid.New(), uuid.New()
	docs := &scopedDocuments{}
	publisher := ScopedPublisher{Documents: docs, Tenant: tenant, Run: run}
	entries := make([]Entry, 513)
	for i := range entries {
		id := uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012x", i+1))
		entries[i] = Entry{Ref: Ref{ArtifactID: id, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Contract: "image/tiff"}
	}
	if _, err := BuildInputClosure(context.Background(), task, &entryCursor{entries: entries}, publisher); err != nil {
		t.Fatal(err)
	}
	if len(docs.scopes) != 3 {
		t.Fatalf("unexpected publication count %d", len(docs.scopes))
	}
	for i, want := range []string{"v2_input_chunk_0", "v2_input_chunk_1", "v2_input_manifest"} {
		s := docs.scopes[i]
		if s.Tenant != tenant || s.Run != run || s.Owner != task || s.Purpose != want {
			t.Fatalf("bad scope %d: %+v", i, s)
		}
	}
}
