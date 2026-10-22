package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ShownRemovalAdoptionParams is the byte-copy worker contract. It carries the
// selected producer fence, not permission to approve or rerender an image.
type ShownRemovalAdoptionParams struct {
	SchemaVersion     int         `json:"schema_version"`
	TenantID          uuid.UUID   `json:"tenant_id"`
	ChildRunID        uuid.UUID   `json:"child_run_id"`
	TargetID          uuid.UUID   `json:"target_id"`
	ApprovedEdit      v2input.Ref `json:"approved_edit"`
	PreviewRunID      uuid.UUID   `json:"preview_run_id"`
	PreviewTaskID     uuid.UUID   `json:"preview_task_id"`
	PreviewResultID   uuid.UUID   `json:"preview_result_id"`
	PreviewAttempt    int         `json:"preview_attempt"`
	PreviewFence      int64       `json:"preview_fence"`
	Presentation      v2input.Ref `json:"presentation"`
	NumericalEvidence v2input.Ref `json:"numerical_evidence"`
	WorkingImage      v2input.Ref `json:"working_image"`
	Pixels            v2input.Ref `json:"pixels"`
	ModelLockSHA      string      `json:"model_lock_sha"`
	RuntimeLockSHA    string      `json:"runtime_lock_sha"`
	DeviceProfileSHA  string      `json:"device_profile_sha"`
}

// FreezeShownRemovalAdoptionParams validates parity with the worker model and
// emits canonical immutable bytes. SQL must separately prove the decision,
// receipt, full input closure and quota within the Apply transaction.
func FreezeShownRemovalAdoptionParams(p ShownRemovalAdoptionParams) ([]byte, string, error) {
	if p.SchemaVersion != 1 || p.PreviewAttempt < 1 || int64(p.PreviewAttempt) > 2147483647 || p.PreviewFence < 1 ||
		p.PreviewTaskID != uuid.NewSHA1(p.PreviewRunID, []byte("semantic.preview_remove_object:"+p.TargetID.String())) {
		return nil, "", ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range []uuid.UUID{p.TenantID, p.ChildRunID, p.TargetID, p.PreviewRunID, p.PreviewTaskID, p.PreviewResultID} {
		if id == uuid.Nil || seen[id] {
			return nil, "", ErrInvalid
		}
		seen[id] = true
	}
	for _, ref := range []v2input.Ref{p.ApprovedEdit, p.Presentation, p.NumericalEvidence, p.WorkingImage, p.Pixels} {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] || !shaPattern.MatchString(ref.SHA256) {
			return nil, "", ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	for _, digest := range []string{p.ModelLockSHA, p.RuntimeLockSHA, p.DeviceProfileSHA} {
		if !shaPattern.MatchString(digest) {
			return nil, "", ErrInvalid
		}
	}
	raw, err := canonical.Marshal(p)
	if err != nil {
		return nil, "", ErrInvalid
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

// ParseShownRemovalAdoptionParams accepts only the exact frozen worker bytes.
// This does not grant Apply authority; the live SQL proof remains mandatory.
func ParseShownRemovalAdoptionParams(raw []byte) (ShownRemovalAdoptionParams, error) {
 var p ShownRemovalAdoptionParams
 if len(raw) < 2 || len(raw) > 1048576 || decodeStrict(raw, &p) != nil {
  return ShownRemovalAdoptionParams{}, ErrInvalid
 }
 body, _, err := FreezeShownRemovalAdoptionParams(p)
 if err != nil || !bytes.Equal(raw, body) {
  return ShownRemovalAdoptionParams{}, ErrInvalid
 }
 return p, nil
}

// MarshalJSON preserves historic safe numeric fences and losslessly encodes full int64.
func (p ShownRemovalAdoptionParams) MarshalJSON() ([]byte, error) {
 type plain ShownRemovalAdoptionParams
 return canonical.MarshalCopyFenceDocument(plain(p))
}

func (p *ShownRemovalAdoptionParams) UnmarshalJSON(raw []byte) error {
 type plain ShownRemovalAdoptionParams
 return canonical.UnmarshalCopyFenceDocument(raw, (*plain)(p))
}
