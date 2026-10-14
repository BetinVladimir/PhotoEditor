package v2draft

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type GlareDonorPreparationRequest struct {
	SchemaVersion     int                           `json:"schema_version"`
	Shoot             uuid.UUID                     `json:"shoot_session_id"`
	Snapshot          uuid.UUID                     `json:"snapshot_id"`
	Ordinal           int                           `json:"ordinal"`
	Base              v2input.SessionVersionBinding `json:"base"`
	Donor             v2input.SessionVersionBinding `json:"donor"`
	Strength          float64                       `json:"strength"`
	GlassesMask       v2input.Ref                   `json:"glasses_mask"`
	Protected         v2input.Ref                   `json:"protected"`
	TargetPixel       v2input.Ref                   `json:"target_pixel"`
	ProtectedPixel    v2input.Ref                   `json:"protected_pixel"`
	SourceFaces       v2input.Ref                   `json:"source_faces"`
	DonorFaces        v2input.Ref                   `json:"donor_faces"`
	SourceObservation uuid.UUID                     `json:"source_observation_id"`
	DonorObservation  uuid.UUID                     `json:"donor_observation_id"`
}

func DecodeGlareDonorPreparationRequest(raw []byte) (GlareDonorPreparationRequest, error) {
	var zero GlareDonorPreparationRequest
	if len(raw) < 2 || len(raw) > 16384 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return zero, v2input.ErrInvalid
	}
	var request GlareDonorPreparationRequest
	if json.Unmarshal(raw, &request) != nil || request.SchemaVersion != 1 ||
		request.Shoot == uuid.Nil || request.Snapshot == uuid.Nil ||
		request.Ordinal < 0 || request.Ordinal >= 5000 ||
		request.SourceObservation == uuid.Nil || request.DonorObservation == uuid.Nil ||
		request.SourceObservation == request.DonorObservation {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(request)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	// Check the wire-level donor and mask invariants before any scheduler read.
	// SQL still establishes observation, same-person and manual-mask authority.
	shape := v2portrait.OpenEyesParams{SchemaVersion: 1, Base: request.Base,
		Donor: request.Donor, Strength: request.Strength,
		SourceFaceID: uuid.NameSpaceOID, DonorFaceID: uuid.NameSpaceDNS,
		EyesMask: request.GlassesMask, Protected: request.Protected,
		Confirmation: request.TargetPixel, SourceFaces: request.SourceFaces,
		DonorFaces: request.DonorFaces}
	if _, _, err := shape.Compile(); err != nil {
		return zero, v2input.ErrInvalid
	}
	refs := []v2input.Ref{request.Base.Source, request.Donor.Source,
		request.GlassesMask, request.Protected, request.TargetPixel,
		request.ProtectedPixel, request.SourceFaces, request.DonorFaces}
	seen := make(map[uuid.UUID]struct{}, len(refs))
	for _, ref := range refs {
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

// PrepareProjectKeyed supplies the owner placeholder and proof digest from
// trusted server inputs. It creates no run; Submit repeats all live checks.
func (s *GlareDonorSubmissionService) PrepareProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	request GlareDonorPreparationRequest) (GlareDonorClientIntent, error) {
	var zero GlareDonorClientIntent
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || tenant != s.tenant || project != s.project ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return zero, err
	}
	request, err = DecodeGlareDonorPreparationRequest(raw)
	if err != nil {
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
	owner, err := v2input.TaskID(workflow, "portrait.glare", request.Ordinal)
	if err != nil {
		return zero, err
	}
	proposal, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "mode": "donor", "base": request.Base,
		"donor": request.Donor, "strength": request.Strength,
		"glasses_mask": request.GlassesMask, "protected": request.Protected,
		"confirmation": v2input.Ref{ArtifactID: owner, SHA256: s.pin.ModelLockSHA},
		"source_faces": request.SourceFaces, "donor_faces": request.DonorFaces,
		"model_lock_sha": s.pin.ModelLockSHA, "calibration": s.pin.Calibration,
	})
	if err != nil {
		return zero, err
	}
	pinRaw, err := s.pin.Compile()
	if err != nil {
		return zero, err
	}
	proof, err := v2portrait.ReadGlareDonorPreparationProof(ctx, s.scheduler,
		who, tenant, project, request.Shoot, request.Snapshot, proposal, pinRaw,
		request.SourceObservation, request.DonorObservation)
	if err != nil {
		return zero, err
	}
	if _, err = v2portrait.ReadManualPairOrigin(ctx, s.scheduler, who,
		tenant, project, request.Snapshot, request.Base, request.GlassesMask,
		request.Protected, request.TargetPixel, request.ProtectedPixel); err != nil {
		return zero, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(proposal, &fields) != nil || fields == nil {
		return zero, v2input.ErrInvalid
	}
	fields["confirmation"], err = canonical.Marshal(proof.Params.Confirmation)
	if err != nil {
		return zero, err
	}
	proposal, err = canonical.Marshal(fields)
	if err != nil {
		return zero, err
	}
	return GlareDonorClientIntent{SchemaVersion: 1, Shoot: request.Shoot,
		Snapshot: request.Snapshot, Ordinal: request.Ordinal, Proposal: proposal,
		SourceObservation: request.SourceObservation,
		DonorObservation:  request.DonorObservation}, nil
}
