package provenance

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// VerifySigningNegativeTransport checks physical transport documents against the
// independently resolved inventory. It does not grant historical object scope;
// the document reader must prove adopted task authority for every read.
func VerifySigningNegativeTransport(ctx context.Context, documents SigningTaskDocumentReader, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask, entries []v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if documents == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil {
		return ErrInvalid
	}
	entries = append([]v2input.Entry(nil), entries...)
	taskBytes, err := canonical.Marshal(task)
	if err != nil {
		return ErrInvalid
	}
	var ownedTask CanonicalSigningTask
	if json.Unmarshal(taskBytes, &ownedTask) != nil {
		return ErrInvalid
	}
	task = ownedTask
	if err := verifySigningNegativeInputDigest(task, entries); err != nil {
		return err
	}
	inputs, err := canonicalTaskOutputRefs(task.Inputs)
	if err != nil || len(inputs) != 5 {
		return ErrInvalid
	}
 direct:=[]v2input.Ref{}
 for _,entry:=range entries{if entry.Contract!="NpyArray"{direct=append(direct,entry.Ref)}}
 if len(direct)!=4{return ErrInvalid}
 for i,pin:=range direct{if inputs[i+1]!=pin{return ErrInvalid}}
	bodies := map[v2input.Ref][]byte{}
	read := func(reference v2input.Ref) ([]byte, error) {
		var readTask CanonicalSigningTask
		if json.Unmarshal(taskBytes, &readTask) != nil {
			return nil, ErrInvalid
		}
		raw, err := documents.ReadSigningTaskDocument(ctx, who, ready, readTask, reference, 1<<20)
		if err != nil {
			return nil, err
		}
		if !pinned(raw, reference, 1<<20) {
			return nil, ErrInvalid
		}
		owned := append([]byte(nil), raw...)
		bodies[reference] = owned
		return owned, nil
	}
	manifestRaw, err := read(inputs[0])
	if err != nil {
		return err
	}
	var manifest v2input.Manifest
	if json.Unmarshal(manifestRaw, &manifest) != nil {
		return ErrInvalid
	}
	encoded, err := canonical.Marshal(manifest)
	if err != nil || !bytes.Equal(encoded, manifestRaw) || manifest.SchemaVersion != 1 || manifest.TaskID != task.Task || manifest.EntryCount != len(entries) || len(manifest.Chunks) != (len(entries)+v2input.ChunkSize-1)/v2input.ChunkSize {
		return ErrInvalid
	}
	entriesSHA, err := canonical.Hash(entries)
	if err != nil || manifest.EntriesSHA != entriesSHA {
		return ErrInvalid
	}
	seen := map[uuid.UUID]bool{inputs[0].ArtifactID: true}
	for _, entry := range entries {
		if seen[entry.Ref.ArtifactID] {
			return ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = true
	}
	for i, chunk := range manifest.Chunks {
		if !ref(chunk) || seen[chunk.ArtifactID] {
			return ErrInvalid
		}
		seen[chunk.ArtifactID] = true
		raw, err := read(chunk)
		if err != nil {
			return err
		}
		end := min((i+1)*v2input.ChunkSize, len(entries))
		expected, err := canonical.Marshal(v2input.Chunk{SchemaVersion: 1, Entries: entries[i*v2input.ChunkSize : end]})
		if err != nil || !bytes.Equal(raw, expected) {
			return ErrInvalid
		}
	}
	for reference, raw := range bodies {
		var readTask CanonicalSigningTask
		if json.Unmarshal(taskBytes, &readTask) != nil {
			return ErrInvalid
		}
		current, err := documents.ReadSigningTaskDocument(ctx, who, ready, readTask, reference, 1<<20)
		if err != nil {
			return err
		}
		if !bytes.Equal(raw, current) {
			return ErrInvalid
		}
	}
	return ctx.Err()
}
