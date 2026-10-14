package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// ReflectionPreparationRequest contains only editor-selected evidence. Model
// and calibration pins are supplied by the operator-owned service.
type ReflectionPreparationRequest struct {
	SchemaVersion  int                           `json:"schema_version"`
	Snapshot       uuid.UUID                     `json:"snapshot_id"`
	Ordinal        int                           `json:"ordinal"`
	Base           v2input.SessionVersionBinding `json:"base"`
	Strength       float64                       `json:"strength"`
	Mask           v2input.Ref                   `json:"mask"`
	Protected      v2input.Ref                   `json:"protected"`
	TargetPixel    v2input.Ref                   `json:"target_pixel"`
	ProtectedPixel v2input.Ref                   `json:"protected_pixel"`
}

type ReflectionClientIntent struct {
	SchemaVersion  int             `json:"schema_version"`
	Snapshot       uuid.UUID       `json:"snapshot_id"`
	Ordinal        int             `json:"ordinal"`
	Proposal       json.RawMessage `json:"params_proposal"`
	TargetPixel    v2input.Ref     `json:"target_pixel"`
	ProtectedPixel v2input.Ref     `json:"protected_pixel"`
}

func DecodeReflectionPreparationRequest(raw []byte) (ReflectionPreparationRequest, error) {
	var zero ReflectionPreparationRequest
	if len(raw) < 2 || len(raw) > 8192 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return zero, v2input.ErrInvalid
	}
	var request ReflectionPreparationRequest
	if json.Unmarshal(raw, &request) != nil || request.SchemaVersion != 1 ||
		request.Snapshot == uuid.Nil || request.Ordinal < 0 || request.Ordinal >= 5000 ||
		request.Base.AssetVersionID == uuid.Nil || math.IsNaN(request.Strength) ||
		math.IsInf(request.Strength, 0) || request.Strength < 0 || request.Strength > 1 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(request)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	for _, digest := range []string{request.Base.CanvasHash} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	seen := make(map[uuid.UUID]struct{}, 5)
	for _, ref := range []v2input.Ref{request.Base.Source, request.Mask,
		request.Protected, request.TargetPixel, request.ProtectedPixel} {
		if ref.ArtifactID == uuid.Nil {
			return zero, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
		if _, exists := seen[ref.ArtifactID]; exists {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = struct{}{}
	}
	return request, nil
}

func DecodeReflectionClientIntent(raw []byte) (ReflectionClientIntent, error) {
	var zero ReflectionClientIntent
	if len(raw) < 2 || len(raw) > 16384 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return zero, v2input.ErrInvalid
	}
	var intent ReflectionClientIntent
	if json.Unmarshal(raw, &intent) != nil || intent.SchemaVersion != 1 ||
		intent.Snapshot == uuid.Nil || intent.Ordinal < 0 || intent.Ordinal >= 5000 ||
		len(intent.Proposal) < 2 || len(intent.Proposal) > 65536 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(intent)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	var proposal struct {
		SchemaVersion int                           `json:"schema_version"`
		Base          v2input.SessionVersionBinding `json:"base"`
		Strength      float64                       `json:"strength"`
		Mask          v2input.Ref                   `json:"mask"`
		Protected     v2input.Ref                   `json:"protected"`
		ModelLockSHA  string                        `json:"model_lock_sha"`
		Calibration   v2input.Ref                   `json:"calibration"`
	}
	if json.Unmarshal(intent.Proposal, &proposal) != nil || proposal.SchemaVersion != 1 {
		return zero, v2input.ErrInvalid
	}
	proposalExact, err := canonical.Marshal(proposal)
	if err != nil || !bytes.Equal(proposalExact, intent.Proposal) {
		return zero, v2input.ErrInvalid
	}
	selection := ReflectionPreparationRequest{SchemaVersion: 1,
		Snapshot: intent.Snapshot, Ordinal: intent.Ordinal, Base: proposal.Base,
		Strength: proposal.Strength, Mask: proposal.Mask, Protected: proposal.Protected,
		TargetPixel: intent.TargetPixel, ProtectedPixel: intent.ProtectedPixel}
	selectionRaw, err := canonical.Marshal(selection)
	if err != nil {
		return zero, err
	}
	if _, err := DecodeReflectionPreparationRequest(selectionRaw); err != nil {
		return zero, err
	}
	if proposal.Calibration.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	for _, digest := range []string{proposal.ModelLockSHA, proposal.Calibration.SHA256} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	return intent, nil
}

// ReflectionPreparationService does not create a run or grant execution. The
// later draft and graph admission must repeat qualification and mask origin.
type ReflectionPreparationService struct {
	Scheduler *pgxpool.Pool
	Tenant    uuid.UUID
	Project   uuid.UUID
	Pin       v2portrait.RestorationQualificationPin
}

func (s ReflectionPreparationService) PrepareProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	request ReflectionPreparationRequest) (ReflectionClientIntent, error) {
	var zero ReflectionClientIntent
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Scheduler == nil || tenant == uuid.Nil || project == uuid.Nil ||
		tenant != s.Tenant || project != s.Project || who.UserID == uuid.Nil ||
		who.SessionID == uuid.Nil || s.Pin.Operation != "image.reflection" {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(request)
	if err != nil {
		return zero, err
	}
	request, err = DecodeReflectionPreparationRequest(body)
	if err != nil {
		return zero, err
	}
	if _, err := s.Pin.Compile(); err != nil {
		return zero, err
	}
	run, err := v2input.RunID(tenant, who.UserID, key)
	if err != nil {
		return zero, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	if _, err := v2input.TaskID(workflow, "image.reflection", request.Ordinal); err != nil {
		return zero, err
	}
	if _, err := v2portrait.ReadManualPairOrigin(ctx, s.Scheduler, who, tenant,
		project, request.Snapshot, request.Base, request.Mask, request.Protected,
		request.TargetPixel, request.ProtectedPixel); err != nil {
		return zero, err
	}
	proposal, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "base": request.Base, "strength": request.Strength,
		"mask": request.Mask, "protected": request.Protected,
		"model_lock_sha": s.Pin.ModelLockSHA, "calibration": s.Pin.Calibration,
	})
	if err != nil {
		return zero, err
	}
	return ReflectionClientIntent{SchemaVersion: 1, Snapshot: request.Snapshot,
		Ordinal: request.Ordinal, Proposal: proposal,
		TargetPixel: request.TargetPixel, ProtectedPixel: request.ProtectedPixel}, nil
}
