package provenance

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestSigningRenderTransportBindsPhysicalChunks(t *testing.T) {
	for _, mode := range []string{"valid", "manifest_task", "entry_sha", "chunk_body", "repeat_changed", "missing_chunk", "extra_field", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			sha := strings.Repeat("a", 64)
			entries := []v2input.Entry{{Ref: v2input.Ref{ArtifactID: uuid.MustParse("00000000-0000-4000-8000-000000000001"), SHA256: sha}, Contract: "WorkingImage"}, {Ref: v2input.Ref{ArtifactID: uuid.MustParse("00000000-0000-4000-8000-000000000002"), SHA256: sha}, Contract: "FrozenRenderRecipe"}}
			task := CanonicalSigningTask{Task: uuid.New(), Operation: "render.compose", Version: "1", Params: entries[1].Ref}
			task.InputDigest, _ = canonical.Hash(struct {
				Task      uuid.UUID       `json:"task"`
				Operation string          `json:"operation"`
				Version   string          `json:"version"`
				Entries   []v2input.Entry `json:"entries"`
			}{task.Task, task.Operation, task.Version, entries})
			chunkBody, _ := canonical.Marshal(v2input.Chunk{SchemaVersion: 1, Entries: entries})
			if mode == "chunk_body" {
				chunkBody = []byte(`{}`)
			}
			chunk := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(chunkBody)}
			entrySHA, _ := canonical.Hash(entries)
			manifest := v2input.Manifest{SchemaVersion: 1, TaskID: task.Task, EntryCount: len(entries), EntriesSHA: entrySHA, Chunks: []v2input.Ref{chunk}}
			if mode == "manifest_task" {
				manifest.TaskID = uuid.New()
			}
			if mode == "entry_sha" {
				manifest.EntriesSHA = strings.Repeat("b", 64)
			}
			if mode == "missing_chunk" {
				manifest.Chunks = []v2input.Ref{}
			}
			manifestBody, _ := canonical.Marshal(manifest)
			if mode == "extra_field" {
				var fields map[string]any
				_ = json.Unmarshal(manifestBody, &fields)
				fields["extra"] = true
				manifestBody, _ = canonical.Marshal(fields)
			}
			manifestRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(manifestBody)}
			task.Inputs, _ = canonical.Marshal([]v2input.Ref{manifestRef, task.Params})
			bodies := map[v2input.Ref][]byte{manifestRef: manifestBody, chunk: chunkBody}
			calls := map[v2input.Ref]int{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			documents := signingDocumentsFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, ref v2input.Ref, _ int) ([]byte, error) {
				calls[ref]++
				if mode == "cancel" {
					cancel()
				}
				if mode == "repeat_changed" && calls[ref] > 1 {
					return []byte(`{}`), nil
				}
				return bodies[ref], nil
			})
			err := VerifySigningRenderTransport(ctx, documents, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New()}, task, entries)
			if mode == "valid" {
				if err != nil || calls[manifestRef] != 2 || calls[chunk] != 2 {
					t.Fatal("valid transport refused", err, calls)
				}
			} else if err == nil {
				t.Fatal("invalid transport accepted")
			}
		})
	}
}

func TestSigningRenderTransportCrossesChunkBoundary(t *testing.T) {
	for _, swapped := range []bool{false, true} {
		entries := make([]v2input.Entry, 513)
		for i := range entries {
			var id uuid.UUID
			binary.BigEndian.PutUint64(id[8:], uint64(i+1))
			entries[i] = v2input.Entry{Ref: v2input.Ref{ArtifactID: id, SHA256: strings.Repeat("a", 64)}, Contract: "Artifact"}
		}
		entries[0].Contract = "FrozenRenderRecipe"
		task := CanonicalSigningTask{Task: uuid.New(), Operation: "render.compose", Version: "1", Params: entries[0].Ref}
		task.InputDigest, _ = canonical.Hash(struct {
			Task      uuid.UUID       `json:"task"`
			Operation string          `json:"operation"`
			Version   string          `json:"version"`
			Entries   []v2input.Entry `json:"entries"`
		}{task.Task, task.Operation, task.Version, entries})
		bodies := map[v2input.Ref][]byte{}
		chunks := []v2input.Ref{}
		for start := 0; start < len(entries); start += 512 {
			body, _ := canonical.Marshal(v2input.Chunk{SchemaVersion: 1, Entries: entries[start:min(start+512, len(entries))]})
			ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(body)}
			bodies[ref] = body
			chunks = append(chunks, ref)
		}
		if swapped {
			chunks[0], chunks[1] = chunks[1], chunks[0]
		}
		sha, _ := canonical.Hash(entries)
		body, _ := canonical.Marshal(v2input.Manifest{SchemaVersion: 1, TaskID: task.Task, EntryCount: len(entries), EntriesSHA: sha, Chunks: chunks})
		manifest := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(body)}
		bodies[manifest] = body
		task.Inputs, _ = canonical.Marshal([]v2input.Ref{manifest, task.Params})
		documents := signingDocumentsFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, e CanonicalSigningTask, r v2input.Ref, _ int) ([]byte, error) {
			// A reader cannot redefine the next read's task or caller inventory.
			e.Inputs[0] = 'x'
			entries[0].Contract = "changed"
			return bodies[r], nil
		})
		err := VerifySigningRenderTransport(context.Background(), documents, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New()}, task, entries)
		if !swapped && err != nil {
			t.Fatal("valid 512+1 transport refused", err)
		}
		if swapped && err == nil {
			t.Fatal("swapped chunks accepted")
		}
	}
}
