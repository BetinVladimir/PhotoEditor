package v2render

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

type RenderBatchPartEvidence struct {
	RunID        uuid.UUID `json:"run_id"`
	FirstOrdinal int       `json:"first_ordinal"`
	FrameCount   int       `json:"frame_count"`
	ConfigSHA    string    `json:"config_sha"`
	OutputBytes  int64     `json:"output_bytes"`
}
type RenderBatchManifest struct {
	PolicySHA     string                    `json:"policy_sha"`
	SchemaVersion int                       `json:"schema_version"`
	ProjectID     uuid.UUID                 `json:"project_id"`
	SnapshotID    uuid.UUID                 `json:"input_snapshot_id"`
	SessionID     uuid.UUID                 `json:"auth_session_id"`
	Parts         []RenderBatchPartEvidence `json:"parts"`
}

func validateRenderBatchManifest(batch uuid.UUID, manifest RenderBatchManifest) error {
	if batch == uuid.Nil || manifest.SchemaVersion != 1 || manifest.ProjectID == uuid.Nil || manifest.SnapshotID == uuid.Nil || manifest.SessionID == uuid.Nil || len(manifest.Parts) < 1 || len(manifest.Parts) > 5000 {
		return v2input.ErrInvalid
	}
	if len(manifest.PolicySHA) != 64 {
		return v2input.ErrInvalid
	}
	for _, c := range manifest.PolicySHA {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return v2input.ErrInvalid
		}
	}
	count := 0
	var output int64
	for i, part := range manifest.Parts {
		if part.RunID != uuid.NewSHA1(batch, []byte("render.batch:"+strconv.Itoa(i))) || part.FirstOrdinal != count || part.FrameCount < 1 || part.FrameCount > 5000-count || part.OutputBytes < 1 || part.OutputBytes > 9007199254740991-output || len(part.ConfigSHA) != 64 {
			return v2input.ErrInvalid
		}
		for _, c := range part.ConfigSHA {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return v2input.ErrInvalid
			}
		}
		count += part.FrameCount
		output += part.OutputBytes
	}
	return nil
}

// BuildRenderBatchManifest authenticates each compiled child before recording
// compact immutable hashes/counts. It does not stage children or reserve quota.
func BuildRenderBatchManifest(batch, project, snapshot uuid.UUID, who auth.Identity, policySHA string, parts []CompiledRenderBatchPart) (json.RawMessage, error) {
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || len(parts) < 1 || len(parts) > 5000 {
		return nil, v2input.ErrInvalid
	}
	manifest := RenderBatchManifest{PolicySHA: policySHA, SchemaVersion: 1, ProjectID: project, SnapshotID: snapshot, SessionID: who.SessionID, Parts: make([]RenderBatchPartEvidence, 0, len(parts))}
	for _, part := range parts {
		saved := RecoveredRenderDraft{RunID: part.RunID, WorkflowID: part.Plan.Workflow, ProjectID: project, SnapshotID: snapshot, Config: part.Plan.Config, ConfigSHA: part.Plan.ConfigSHA, ReservedBytes: part.Plan.OutputBytes}
		nodes, err := DecodeRecoveredRenderPlan(saved)
		if err != nil {
			return nil, err
		}
		if len(nodes) != len(part.Plan.Tasks) {
			return nil, v2input.ErrInvalid
		}
		var config struct {
			Render struct {
				Session uuid.UUID `json:"auth_session_id"`
			} `json:"v26_render"`
		}
		if json.Unmarshal(part.Plan.Config, &config) != nil || config.Render.Session != who.SessionID {
			return nil, v2input.ErrInvalid
		}
		for i, node := range nodes {
			if node.Node.TaskID != part.Plan.Tasks[i].ID {
				return nil, v2input.ErrInvalid
			}
		}
		manifest.Parts = append(manifest.Parts, RenderBatchPartEvidence{RunID: part.RunID, FirstOrdinal: part.FirstOrdinal, FrameCount: len(nodes), ConfigSHA: part.Plan.ConfigSHA, OutputBytes: part.Plan.OutputBytes})
	}
	if err := validateRenderBatchManifest(batch, manifest); err != nil {
		return nil, err
	}
	raw, err := canonical.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	if len(raw) > 1048576 {
		return nil, v2input.ErrInvalid
	}
	return raw, nil
}

// DecodeRenderBatchManifest verifies persisted compact evidence including exact
// field inventory, canonical digest, deterministic child IDs and contiguous range.
func DecodeRenderBatchManifest(batch, session uuid.UUID, raw []byte, sha string) (RenderBatchManifest, error) {
	if len(raw) < 2 || len(raw) > 1048576 || session == uuid.Nil {
		return RenderBatchManifest{}, v2input.ErrInvalid
	}
	body, err := canonical.Bytes(raw)
	if err != nil {
		return RenderBatchManifest{}, err
	}
	digest, err := canonical.Hash(json.RawMessage(body))
	if err != nil || digest != sha {
		return RenderBatchManifest{}, v2input.ErrInvalid
	}
	var result RenderBatchManifest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return RenderBatchManifest{}, v2input.ErrInvalid
	}
	typed, err := canonical.Marshal(result)
	if err != nil || !bytes.Equal(typed, body) || result.SessionID != session {
		return RenderBatchManifest{}, v2input.ErrInvalid
	}
	if err := validateRenderBatchManifest(batch, result); err != nil {
		return RenderBatchManifest{}, err
	}
	return result, nil
}
