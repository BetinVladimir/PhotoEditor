package v2draft

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// LockCalibratedDonorSession joins retained Calibration, installed runtime and
// face evidence with current qualification and consent/source locks. The caller
// must verify the original trusted report bytes before entering admission and
// retain this transaction through graph sealing. It is not a graph admission.
func LockCalibratedDonorSession(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant, project, shoot uuid.UUID,
	loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence, located v2portrait.LocatedDonorQualification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	qualification, err := located.Qualification()
	if err != nil {
		return err
	}
	pin, err := qualification.Pin()
	if err != nil {
		return err
	}
	preparedPin, err := verifyPreparedDonorQualification(loaded, prepared)
	if err != nil {
		return err
	}
	if preparedPin != pin {
		return v2input.ErrInvalid
	}
	calibration := prepared.calibration
	if calibration == nil || pin.Calibration != calibration.ref || pin.ModelLockSHA != calibration.modelLockSHA || pin.DatasetSHA != calibration.datasetSHA || pin.RuntimeLockSHA != loaded.runtimeSHA {
		return v2input.ErrInvalid
	}
	if err := calibration.verify(prepared.Faces.minEdge); err != nil {
		return err
	}
	if err := v2portrait.LockLocatedDonorQualification(ctx, tx, who, tenant, project, located); err != nil {
		return err
	}
	return LockLoadedDonorFaceSession(ctx, tx, who, tenant, project, shoot, loaded, prepared.Faces)
}
