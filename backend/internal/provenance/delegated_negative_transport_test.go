package provenance

import (
 "context"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedNegativeTransportBindsCanonicalChunks(t *testing.T) {
 for _, mode := range []string{"valid", "manifest_task", "chunk_body", "repeat_changed", "cancel"} {
  t.Run(mode, func(t *testing.T) {
   entries := []v2input.Entry{}
   for i, contract := range []string{"Params", "WorkingImage", "NegativeCalibration", "QualificationReport", "NpyArray"} {
    id := uuid.UUID{}; id[15] = byte(i+1)
    entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: id, SHA256: strings.Repeat("a", 64)}, Contract: contract})
   }
   task := CanonicalSigningTask{Task: uuid.New(), Operation: "optical.negative", Version: "1", Params: entries[0].Ref}
   task.InputDigest, _ = canonical.Hash(struct {
    Task uuid.UUID `json:"task"`
    Operation string `json:"operation"`
    Version string `json:"version"`
    Entries []v2input.Entry `json:"entries"`
   }{task.Task, task.Operation, task.Version, entries})
   chunkBody, _ := canonical.Marshal(v2input.Chunk{SchemaVersion: 1, Entries: entries})
   if mode == "chunk_body" { chunkBody = []byte(`{}`) }
   chunk := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(chunkBody)}
   entrySHA, _ := canonical.Hash(entries)
   manifest := v2input.Manifest{SchemaVersion: 1, TaskID: task.Task, EntryCount: len(entries), EntriesSHA: entrySHA, Chunks: []v2input.Ref{chunk}}
   if mode == "manifest_task" { manifest.TaskID = uuid.New() }
   manifestBody, _ := canonical.Marshal(manifest)
   manifestRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(manifestBody)}
   task.Inputs, _ = canonical.Marshal([]v2input.Ref{manifestRef, entries[0].Ref, entries[1].Ref, entries[2].Ref, entries[3].Ref})
   bodies := map[v2input.Ref][]byte{manifestRef: manifestBody, chunk: chunkBody}
   calls := map[v2input.Ref]int{}
   ctx, cancel := context.WithCancel(context.Background()); defer cancel()
   documents := delegatedDocumentsFunc(func(_ context.Context, _ CanonicalSigningTask, ref v2input.Ref, _ int) ([]byte, error) {
    calls[ref]++
    if mode == "cancel" { cancel() }
    if mode == "repeat_changed" && calls[ref] > 1 { return []byte(`{}`), nil }
    return bodies[ref], nil
   })
   err := VerifyDelegatedNegativeTransport(ctx, documents, task, entries)
   if mode == "valid" {
    if err != nil || calls[manifestRef] != 2 || calls[chunk] != 2 { t.Fatal("valid delegated transport refused", err, calls) }
   } else if err == nil { t.Fatal("invalid delegated transport accepted", mode) }
  })
 }
}
