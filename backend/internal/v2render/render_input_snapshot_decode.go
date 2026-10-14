package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

func RenderInputSnapshotID(tid, actor, pid uuid.UUID, key string) (uuid.UUID, error) {
	if tid == uuid.Nil || actor == uuid.Nil || pid == uuid.Nil || !idempotency.ValidKey(key) {
		return uuid.Nil, v2input.ErrInvalid
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("v26-render-input/"+tid.String()+"/"+actor.String()+"/"+pid.String()+"/"+key)), nil
}

// The public intent is exactly {sources:[SessionVersionBinding]}, never a
// collection selection snapshot or client-supplied execution policy.
func DecodeRenderInputSnapshotRequest(raw []byte) ([]v2input.SessionVersionBinding, error) {
	if len(raw) == 0 || len(raw) > MaxRenderSubmissionBytes {
		return nil, v2input.ErrInvalid
	}
	body, err := canonical.Bytes(raw)
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	root, err := renderSubmissionFields(body, "sources")
	if err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if json.Unmarshal(root["sources"], &items) != nil || len(items) < 1 || len(items) > 5000 {
		return nil, v2input.ErrInvalid
	}
	for _, item := range items {
		source, err := renderSubmissionFields(item, "asset_version_id", "source", "canvas_hash")
		if err != nil {
			return nil, err
		}
		if _, err := renderSubmissionFields(source["source"], "artifact_id", "sha256"); err != nil {
			return nil, err
		}
	}
	var sources []v2input.SessionVersionBinding
	if json.Unmarshal(root["sources"], &sources) != nil {
		return nil, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, source := range sources {
		if source.AssetVersionID == uuid.Nil || !validLayerArtifactRef(source.Source) || seen[source.AssetVersionID] {
			return nil, v2input.ErrInvalid
		}
		seen[source.AssetVersionID] = true
		if len(source.CanvasHash) != 64 {
			return nil, v2input.ErrInvalid
		}
		for _, ch := range source.CanvasHash {
			if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
				return nil, v2input.ErrInvalid
			}
		}
	}
	return sources, nil
}

func DecodeRenderInputSnapshotResult(raw []byte, id uuid.UUID, digest string, count int) (RenderInputSnapshot, error) {
	var result RenderInputSnapshot
	body, err := canonical.Bytes(raw)
	if err != nil {
		return result, v2input.ErrInvalid
	}
	if _, err := renderSubmissionFields(body, "id", "digest", "item_count"); err != nil {
		return result, err
	}
	if json.Unmarshal(body, &result) != nil || id == uuid.Nil || count < 1 || count > 5000 || result.ID != id || result.Digest != digest || result.ItemCount != count {
		return RenderInputSnapshot{}, v2input.ErrInvalid
	}
	return result, nil
}
