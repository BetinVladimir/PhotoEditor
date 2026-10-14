package workflow

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// AdmissionJSON validates the entire private publication result against the
// current original plan; SQL must independently validate ready stored objects.
func (c PerspectiveLinesClosure) AdmissionJSON(plan PerspectiveLinesPlan, params review.ArtifactRef) (json.RawMessage, error) {
	entries, err := plan.Entries(params)
	if err != nil {
		return nil, err
	}
	if c.task != plan.task || c.built.Count != len(entries) || len(c.built.Chunks) != 1 || len(c.documents) != 2 {
		return nil, v2input.ErrInvalid
	}
	exactEntries, err := canonical.Marshal(entries)
	if err != nil {
		return nil, err
	}
	actualEntries, err := canonical.Marshal(c.entries)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(exactEntries, actualEntries) || c.built.SHA256 != renderDerivativeBytesSHA(exactEntries) {
		return nil, v2input.ErrInvalid
	}
	chunk := c.built.Chunks[0]
	manifest := c.built.Manifest
	seen := map[uuid.UUID]bool{}
	for _, entry := range entries {
		seen[entry.Ref.ArtifactID] = true
	}
	for _, ref := range []v2input.Ref{chunk, manifest} {
		if ref.ArtifactID == uuid.Nil || domainDigestInvalid(ref.SHA256) || seen[ref.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	chunkRaw := c.documents[chunk.ArtifactID]
	manifestRaw := c.documents[manifest.ArtifactID]
	if renderDerivativeBytesSHA(chunkRaw) != chunk.SHA256 || renderDerivativeBytesSHA(manifestRaw) != manifest.SHA256 {
		return nil, v2input.ErrInvalid
	}
	if _, err := CompilePerspectiveLinesTransportDocument(plan, params, 0, "InputClosureChunk", chunkRaw, nil); err != nil {
		return nil, err
	}
	if _, err := CompilePerspectiveLinesTransportDocument(plan, params, 1, "InputClosureManifest", manifestRaw, &chunk); err != nil {
		return nil, err
	}
	return canonical.Marshal(map[string]any{"schema_version": 1, "task_id": plan.task, "entries": entries, "entry_count": len(entries), "entries_sha": c.built.SHA256, "chunks": []v2input.Ref{chunk}, "manifest": manifest, "chunk_jcs": string(chunkRaw), "manifest_jcs": string(manifestRaw)})
}
