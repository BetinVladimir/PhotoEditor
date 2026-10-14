package provenance

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// VerifyDelegatedNegativeTransport verifies the canonical manifest/chunks and
// their exact repeatable task-scoped document bytes without an editor session.
func VerifyDelegatedNegativeTransport(ctx context.Context, documents DelegatedTaskDocuments,
 task CanonicalSigningTask, entries []v2input.Entry) error {
 if err := ctx.Err(); err != nil { return err }
 if documents == nil { return ErrInvalid }
 owned, err := ownSigningProducerTask(task)
 if err != nil { return err }
 task = owned
 entries = append([]v2input.Entry(nil), entries...)
 if err := verifySigningNegativeInputDigest(task, entries); err != nil { return err }
 inputs, err := canonicalTaskOutputRefs(task.Inputs)
 if err != nil || len(inputs) != 5 { return ErrInvalid }
 direct := []v2input.Ref{}
 for _, entry := range entries { if entry.Contract != "NpyArray" { direct = append(direct, entry.Ref) } }
 if len(direct) != 4 { return ErrInvalid }
 for i, pin := range direct { if inputs[i+1] != pin { return ErrInvalid } }
 bodies := map[v2input.Ref][]byte{}
 read := func(reference v2input.Ref) ([]byte, error) {
  if err := ctx.Err(); err != nil { return nil, err }
  isolated, err := ownSigningProducerTask(task)
  if err != nil { return nil, err }
  raw, err := documents.ReadDelegatedTaskDocument(ctx, isolated, reference, 1<<20)
  if err != nil { return nil, err }
  if !pinned(raw, reference, 1<<20) { return nil, ErrInvalid }
  bodies[reference] = append([]byte(nil), raw...)
  return raw, nil
 }
 manifestRaw, err := read(inputs[0]); if err != nil { return err }
 var manifest v2input.Manifest
 if json.Unmarshal(manifestRaw, &manifest) != nil { return ErrInvalid }
 encoded, err := canonical.Marshal(manifest)
 if err != nil || !bytes.Equal(encoded, manifestRaw) || manifest.SchemaVersion != 1 ||
  manifest.TaskID != task.Task || manifest.EntryCount != len(entries) ||
  len(manifest.Chunks) != (len(entries)+v2input.ChunkSize-1)/v2input.ChunkSize { return ErrInvalid }
 entriesSHA, err := canonical.Hash(entries)
 if err != nil || manifest.EntriesSHA != entriesSHA { return ErrInvalid }
 seen := map[uuid.UUID]bool{inputs[0].ArtifactID: true}
 for _, entry := range entries {
  if seen[entry.Ref.ArtifactID] { return ErrInvalid }
  seen[entry.Ref.ArtifactID] = true
 }
 for i, chunk := range manifest.Chunks {
  if !ref(chunk) || seen[chunk.ArtifactID] { return ErrInvalid }
  seen[chunk.ArtifactID] = true
  raw, err := read(chunk); if err != nil { return err }
  end := min((i+1)*v2input.ChunkSize, len(entries))
  expected, err := canonical.Marshal(v2input.Chunk{SchemaVersion: 1,
   Entries: entries[i*v2input.ChunkSize:end]})
  if err != nil || !bytes.Equal(raw, expected) { return ErrInvalid }
 }
 for reference, original := range bodies {
  if err := ctx.Err(); err != nil { return err }
  isolated, err := ownSigningProducerTask(task)
  if err != nil { return err }
  current, err := documents.ReadDelegatedTaskDocument(ctx, isolated, reference, 1<<20)
  if err != nil { return err }
  if !bytes.Equal(original, current) { return ErrInvalid }
 }
 return ctx.Err()
}
