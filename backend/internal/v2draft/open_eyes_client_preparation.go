package v2draft

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// OpenEyesClientPreparation is a proposal, not an issued confirmation or an
// admitted task. Its proof digest is recomputed under live SQL locks on submit.
type OpenEyesClientPreparation struct {
	SchemaVersion     int                       `json:"schema_version"`
	Shoot             uuid.UUID                 `json:"shoot_session_id"`
	Snapshot          uuid.UUID                 `json:"snapshot_id"`
	Ordinal           int                       `json:"ordinal"`
	Proposal          v2portrait.OpenEyesParams `json:"params_proposal"`
	TargetPixel       v2input.Ref               `json:"target_pixel"`
	ProtectedPixel    v2input.Ref               `json:"protected_pixel"`
	SourceObservation uuid.UUID                 `json:"source_observation_id"`
	DonorObservation  uuid.UUID                 `json:"donor_observation_id"`
}

type OpenEyesPreparationService struct {
	scheduler *pgxpool.Pool
	tenant    uuid.UUID
	project   uuid.UUID
	pin       v2portrait.DonorQualificationPin
}

func NewOpenEyesPreparationService(scheduler *pgxpool.Pool, tenant, project uuid.UUID,
	pin v2portrait.DonorQualificationPin) (*OpenEyesPreparationService, error) {
	if scheduler == nil || tenant == uuid.Nil || project == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	if _, err := pin.Compile(); err != nil {
		return nil, err
	}
	return &OpenEyesPreparationService{scheduler: scheduler, tenant: tenant, project: project, pin: pin}, nil
}

func openEyesProvisional(selection OpenEyesClientSelection, owner uuid.UUID) (v2portrait.OpenEyesParams, error) {
	if owner == uuid.Nil {
		return v2portrait.OpenEyesParams{}, v2input.ErrInvalid
	}
	params := v2portrait.OpenEyesParams{SchemaVersion: 1, Base: selection.Base,
		Donor: selection.Donor, Strength: selection.Strength,
		SourceFaceID: uuid.NameSpaceOID, DonorFaceID: uuid.NameSpaceDNS,
		EyesMask: selection.EyesMask, Protected: selection.Protected,
		Confirmation: v2input.Ref{ArtifactID: owner,
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		SourceFaces: selection.SourceFaces, DonorFaces: selection.DonorFaces}
	if _, _, err := params.Compile(); err != nil {
		return v2portrait.OpenEyesParams{}, err
	}
	return params, nil
}

func (s *OpenEyesPreparationService) PrepareProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	selection OpenEyesClientSelection) (OpenEyesClientPreparation, error) {
	var zero OpenEyesClientPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || tenant != s.tenant || project != s.project ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(selection)
	if err != nil {
		return zero, err
	}
	selection, err = DecodeOpenEyesClientSelection(raw)
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
	owner, err := v2input.TaskID(workflow, "portrait.open_eyes", selection.Ordinal)
	if err != nil {
		return zero, err
	}
	provisional, err := openEyesProvisional(selection, owner)
	if err != nil {
		return zero, err
	}
	proof, err := v2portrait.ReadOpenEyesPreparationProof(ctx, s.scheduler, who,
		tenant, project, selection.Shoot, selection.Snapshot, provisional, s.pin,
		selection.SourceObservation, selection.DonorObservation)
	if err != nil {
		return zero, err
	}
	if _, err = v2portrait.ReadManualPairOrigin(ctx, s.scheduler, who, tenant,
		project, selection.Snapshot, selection.Base, selection.EyesMask,
		selection.Protected, selection.TargetPixel, selection.ProtectedPixel); err != nil {
		return zero, err
	}
	return OpenEyesClientPreparation{SchemaVersion: 1, Shoot: selection.Shoot,
		Snapshot: selection.Snapshot, Ordinal: selection.Ordinal, Proposal: proof.Params,
		TargetPixel: selection.TargetPixel, ProtectedPixel: selection.ProtectedPixel,
		SourceObservation: selection.SourceObservation,
		DonorObservation:  selection.DonorObservation}, nil
}
