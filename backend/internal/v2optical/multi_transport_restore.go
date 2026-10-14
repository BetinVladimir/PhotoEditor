package v2optical

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// RestoreMultiTransport verifies each recovered chunk against the qualified
// immutable descriptor. It grants no live SQL or physical object authority.
func RestoreMultiTransport(ctx context.Context, draft MultiDraft, qualified VerifiedMultiQualification,
	params v2input.Ref, raw []byte) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var wire struct {
		Manifest v2input.Ref   `json:"manifest"`
		Chunks   []v2input.Ref `json:"chunks"`
		Count    int           `json:"count"`
		SHA      string        `json:"sha256"`
	}
	if json.Unmarshal(normal, &wire) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(exact, normal) || !validPerspectiveRef(wire.Manifest) || len(wire.Chunks) < 1 || len(wire.Chunks) > 8 {
		return zero, v2input.ErrInvalid
	}
	descriptor, err := draft.Descriptor(ctx, qualified, params)
	if err != nil {
		return zero, err
	}
	var frozen struct {
		Task    v2input.DraftTask `json:"task"`
		Entries []v2input.Entry   `json:"entries"`
	}
	if json.Unmarshal(descriptor, &frozen) != nil || len(frozen.Entries) != wire.Count || (wire.Count+v2input.ChunkSize-1)/v2input.ChunkSize != len(wire.Chunks) {
		return zero, v2input.ErrInvalid
	}
	sha, err := canonical.Hash(frozen.Entries)
	if err != nil || sha != wire.SHA {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{wire.Manifest.ArtifactID: true}
	for i, ref := range wire.Chunks {
		if !validPerspectiveRef(ref) || seen[ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
		end := min((i+1)*v2input.ChunkSize, wire.Count)
		sha, err := canonical.Hash(v2input.Chunk{SchemaVersion: 1, Entries: frozen.Entries[i*v2input.ChunkSize : end]})
		if err != nil || sha != ref.SHA256 {
			return zero, v2input.ErrInvalid
		}
	}
	sha, err = canonical.Hash(v2input.Manifest{SchemaVersion: 1, TaskID: frozen.Task.ID, EntryCount: wire.Count, EntriesSHA: wire.SHA, Chunks: wire.Chunks})
	if err != nil || sha != wire.Manifest.SHA256 {
		return zero, v2input.ErrInvalid
	}
	direct := []v2input.Ref{}
	for _, entry := range frozen.Entries {
		if seen[entry.Ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		if entry.Contract != "NpyArray" {
			direct = append(direct, entry.Ref)
		}
	}
	entries := append([]v2input.Entry(nil), frozen.Entries...)
	return v2input.GraphTask{ID: frozen.Task.ID, Built: v2input.Built{Manifest: wire.Manifest, Chunks: append([]v2input.Ref(nil), wire.Chunks...), Count: wire.Count, SHA256: wire.SHA}, Direct: direct,
		Open: func(ctx context.Context) (v2input.Cursor, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return &perspectiveEntryCursor{entries: append([]v2input.Entry(nil), entries...)}, nil
		}}, nil
}
