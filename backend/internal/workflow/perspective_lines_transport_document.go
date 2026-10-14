package workflow

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// This owned document is a publication plan, not SQL permission or IO evidence.
// Only the exact one-chunk transport compiled from the original source is valid.
type PerspectiveLinesTransportDocument struct {
	scope artifactjson.Scope
	body  []byte
	sha   string
}

func (d PerspectiveLinesTransportDocument) Scope() artifactjson.Scope { return d.scope }
func (d PerspectiveLinesTransportDocument) Bytes() []byte             { return bytes.Clone(d.body) }
func (d PerspectiveLinesTransportDocument) SHA256() string            { return d.sha }

func CompilePerspectiveLinesTransportDocument(plan PerspectiveLinesPlan, params review.ArtifactRef, ordinal int, contract string, raw []byte, chunk *v2input.Ref) (PerspectiveLinesTransportDocument, error) {
	var zero PerspectiveLinesTransportDocument
	entries, err := plan.Entries(params)
	if err != nil {
		return zero, err
	}
	if len(entries) < 3 || len(entries) > 132 || len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	expectedChunk, err := canonical.Marshal(v2input.Chunk{SchemaVersion: 1, Entries: entries})
	if err != nil {
		return zero, err
	}
	var expected []byte
	var purpose string
	switch contract {
	case "InputClosureChunk":
		if ordinal != 0 || chunk != nil {
			return zero, v2input.ErrInvalid
		}
		expected = expectedChunk
		purpose = "v2_input_chunk_0"
	case "InputClosureManifest":
		if ordinal != 1 || chunk == nil || chunk.ArtifactID == uuid.Nil || chunk.SHA256 != renderDerivativeBytesSHA(expectedChunk) {
			return zero, v2input.ErrInvalid
		}
		for _, entry := range entries {
			if entry.Ref.ArtifactID == chunk.ArtifactID {
				return zero, v2input.ErrInvalid
			}
		}
		entryBytes, err := canonical.Marshal(entries)
		if err != nil {
			return zero, err
		}
		expected, err = canonical.Marshal(v2input.Manifest{SchemaVersion: 1, TaskID: plan.task, EntryCount: len(entries), EntriesSHA: renderDerivativeBytesSHA(entryBytes), Chunks: []v2input.Ref{*chunk}})
		if err != nil {
			return zero, err
		}
		purpose = "v2_input_manifest"
	default:
		return zero, v2input.ErrInvalid
	}
	if !bytes.Equal(raw, expected) {
		return zero, v2input.ErrInvalid
	}
	return PerspectiveLinesTransportDocument{scope: artifactjson.Scope{Tenant: plan.scope.Tenant, Run: plan.scope.Run, Owner: plan.task, Purpose: purpose}, body: bytes.Clone(raw), sha: renderDerivativeBytesSHA(raw)}, nil
}

// Transport body is retained as an original string; JSONB reserialization must
// not replace bytes whose SHA is already bound to a chunk or manifest.
func (d PerspectiveLinesTransportDocument) Proof() (json.RawMessage, error) {
	if d.scope.Tenant == uuid.Nil || d.scope.Run == uuid.Nil || d.scope.Owner == uuid.Nil || len(d.body) < 2 || renderDerivativeBytesSHA(d.body) != d.sha {
		return nil, v2input.ErrInvalid
	}
	return canonical.Marshal(map[string]any{"schema_version": 1, "tenant_id": d.scope.Tenant, "run_id": d.scope.Run, "task_id": d.scope.Owner, "purpose": d.scope.Purpose, "body_jcs": string(d.body), "sha256": d.sha, "bytes": len(d.body)})
}
