package v2draft

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

func verifyPreparedDonorQualification(loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence) (v2portrait.DonorQualificationPin, error) {
	var zero v2portrait.DonorQualificationPin
	if prepared.qualification == nil || prepared.calibration == nil {
		return zero, v2input.ErrInvalid
	}
	verified, err := prepared.qualification.Qualification()
	if err != nil {
		return zero, err
	}
	pin, err := verified.Pin()
	if err != nil {
		return zero, err
	}
	body, err := verified.CalibrationBytes()
	if err != nil {
		return zero, err
	}
	calibration := prepared.calibration
	if pin.Calibration != calibration.ref || pin.ModelLockSHA != calibration.modelLockSHA || pin.DatasetSHA != calibration.datasetSHA || pin.RuntimeLockSHA != loaded.runtimeSHA || !bytes.Equal(body, calibration.raw) {
		return zero, v2input.ErrInvalid
	}
	if err := calibration.verify(prepared.Faces.minEdge); err != nil {
		return zero, err
	}
	return pin, nil
}

// PrepareQualifiedDonorInputEvidence joins authorized original qualification
// objects with mask/face/image evidence. Final transactional admission remains
// mandatory; a successfully prepared inventory does not queue work.
func (s ClosureResolver) PrepareQualifiedDonorInputEvidence(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	loaded LoadedDonorTaskPlan, donorCatalog ArtifactCatalog, manual DonorManualProducer, located v2portrait.LocatedDonorQualification) (PreparedDonorInputEvidence, error) {
	var zero PreparedDonorInputEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	verified, err := located.Qualification()
	if err != nil {
		return zero, err
	}
	pin, err := verified.Pin()
	if err != nil {
		return zero, err
	}
	if pin.RuntimeLockSHA != loaded.runtimeSHA {
		return zero, v2input.ErrInvalid
	}
	raw, err := verified.CalibrationBytes()
	if err != nil {
		return zero, err
	}
	prepared, err := s.PrepareCalibratedDonorInputEvidence(ctx, who, tenant, loaded, donorCatalog, manual, pin.Calibration, raw, pin.ModelLockSHA, pin.DatasetSHA)
	if err != nil {
		return zero, err
	}
	for _, entry := range prepared.Entries {
		if entry.Ref.ArtifactID == pin.Report.ArtifactID {
			return zero, v2input.ErrInvalid
		}
	}
	entries, err := uniqueTaskInputs(append(prepared.Entries, v2input.Entry{Ref: pin.Report, Contract: "Report"}))
	if err != nil {
		return zero, err
	}
	if len(entries) > 126 {
		return zero, v2input.ErrInvalid
	}
	prepared.Entries, prepared.qualification = entries, &located
	if _, err := verifyPreparedDonorQualification(loaded, prepared); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return prepared, nil
}
