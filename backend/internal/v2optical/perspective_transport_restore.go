package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// RestorePublishedTransport checks recovery metadata against the original
// qualified descriptor. It provides no SQL scope or physical object authority.
func (p PreparedPerspective) RestorePublishedTransport(ctx context.Context, raw []byte) (v2input.GraphTask, error) {
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
	if err != nil || !bytes.Equal(exact, normal) || !validPerspectiveRef(wire.Manifest) || len(wire.Chunks) != 1 || !validPerspectiveRef(wire.Chunks[0]) || wire.Manifest.ArtifactID == wire.Chunks[0].ArtifactID {
		return zero, v2input.ErrInvalid
	}
	descriptor, err := p.Descriptor(ctx)
	if err != nil {
		return zero, err
	}
	var frozen struct {
		Task    v2input.DraftTask `json:"task"`
		Entries []v2input.Entry   `json:"entries"`
	}
	if json.Unmarshal(descriptor, &frozen) != nil || len(frozen.Entries) != wire.Count {
		return zero, v2input.ErrInvalid
	}
	entriesSHA, err := canonical.Hash(frozen.Entries)
	if err != nil || entriesSHA != wire.SHA {
		return zero, v2input.ErrInvalid
	}
	chunkSHA, err := canonical.Hash(v2input.Chunk{SchemaVersion: 1, Entries: frozen.Entries})
	if err != nil || chunkSHA != wire.Chunks[0].SHA256 {
		return zero, v2input.ErrInvalid
	}
	manifestSHA, err := canonical.Hash(v2input.Manifest{SchemaVersion: 1, TaskID: frozen.Task.ID, EntryCount: wire.Count, EntriesSHA: wire.SHA, Chunks: wire.Chunks})
	if err != nil || manifestSHA != wire.Manifest.SHA256 {
		return zero, v2input.ErrInvalid
	}
	var direct []v2input.Ref
	for _, entry := range frozen.Entries {
		if entry.Ref.ArtifactID == wire.Manifest.ArtifactID || entry.Ref.ArtifactID == wire.Chunks[0].ArtifactID {
			return zero, v2input.ErrInvalid
		}
		if entry.Contract != "NpyArray" {
			direct = append(direct, entry.Ref)
		}
	}
	snapshot, err := p.draft.Snapshot()
	if err != nil {
		return zero, err
	}
	expectedRoots := 4
	if snapshot.LinesJCS != "" {
		expectedRoots = 5
	}
	if len(direct) != expectedRoots {
		return zero, v2input.ErrInvalid
	}
	entries := append([]v2input.Entry(nil), frozen.Entries...)
	return v2input.GraphTask{ID: frozen.Task.ID, Built: v2input.Built{Manifest: wire.Manifest, Chunks: append([]v2input.Ref(nil), wire.Chunks...), Count: wire.Count, SHA256: wire.SHA}, Direct: direct, Open: func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &perspectiveEntryCursor{entries: append([]v2input.Entry(nil), entries...)}, nil
	}}, nil
}
