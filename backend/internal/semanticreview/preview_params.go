package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ExposurePreviewParams is the independent pre-Apply worker document. It has
// no approval, child run, or decision fields. Callers must resolve all bindings
// from authenticated canonical plan documents and recheck authority in SQL.
type ExposurePreviewParams struct {
	SchemaVersion    int                           `json:"schema_version"`
	TenantID         uuid.UUID                     `json:"tenant_id"`
	IntentID         uuid.UUID                     `json:"intent_id"`
	TargetID         uuid.UUID                     `json:"target_id"`
	WorkflowID       uuid.UUID                     `json:"workflow_id"`
	WorkflowRevision string                        `json:"workflow_revision"`
	TargetRevision   string                        `json:"target_revision"`
	SnapshotID       uuid.UUID                     `json:"snapshot_id"`
	Plan             v2input.Ref                   `json:"plan"`
	Intent           v2input.Ref                   `json:"intent"`
	Candidate        v2input.Ref                   `json:"candidate"`
	Source           v2input.SessionVersionBinding `json:"source"`
	Protection       v2input.Ref                   `json:"protection"`
	ModelLockSHA     string                        `json:"model_lock_sha"`
	RuntimeLockSHA   string                        `json:"runtime_lock_sha"`
	DeviceProfileSHA string                        `json:"device_profile_sha"`
}

// FreezeExposurePreviewParams emits the same canonical document consumed by
// SemanticExposurePreviewInput. Transport validity does not admit a task.
func FreezeExposurePreviewParams(p ExposurePreviewParams) ([]byte, string, error) {
	if p.SchemaVersion != 1 || !previewRevision(p.WorkflowRevision) || !previewRevision(p.TargetRevision) {
		return nil, "", ErrInvalid
	}
	for _, id := range []uuid.UUID{p.TenantID, p.IntentID, p.TargetID, p.WorkflowID, p.SnapshotID, p.Source.AssetVersionID} {
		if id == uuid.Nil {
			return nil, "", ErrInvalid
		}
	}
	for _, digest := range []string{p.Source.CanvasHash, p.ModelLockSHA, p.RuntimeLockSHA, p.DeviceProfileSHA} {
		if !shaPattern.MatchString(digest) {
			return nil, "", ErrInvalid
		}
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range []v2input.Ref{p.Plan, p.Intent, p.Candidate, p.Source.Source, p.Protection} {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] || !shaPattern.MatchString(ref.SHA256) {
			return nil, "", ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	body, err := canonical.Marshal(p)
	if err != nil {
		return nil, "", ErrInvalid
	}
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:]), nil
}
