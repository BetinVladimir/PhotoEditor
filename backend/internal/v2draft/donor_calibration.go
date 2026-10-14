package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2calibration"
	"photoeditor/backend/internal/v2input"
)

type donorCalibrationEvidence struct {
	ref                      v2input.Ref
	raw                      []byte
	modelLockSHA, datasetSHA string
}

func (p donorCalibrationEvidence) verify(minEdge float64) error {
	if !isDocumentRef(p.ref) {
		return v2input.ErrInvalid
	}
	edge, err := v2calibration.CalibrationMinFaceEdge(p.raw, p.ref.SHA256, p.modelLockSHA, p.datasetSHA)
	if err != nil {
		return err
	}
	if float64(edge) != minEdge {
		return v2input.ErrInvalid
	}
	return nil
}

// PrepareCalibratedDonorInputEvidence retains exact calibration bytes and adds
// their distinct artifact to the task inventory. The caller must first load the
// document through its authorized calibration catalog; this compiler does not
// establish live qualification authority or replace the final SQL admission.
func (s ClosureResolver) PrepareCalibratedDonorInputEvidence(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	loaded LoadedDonorTaskPlan, donorCatalog ArtifactCatalog, manual DonorManualProducer,
	calibrationRef v2input.Ref, calibrationRaw []byte, modelLockSHA, datasetSHA string) (PreparedDonorInputEvidence, error) {
	var zero PreparedDonorInputEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !isDocumentRef(calibrationRef) {
		return zero, v2input.ErrInvalid
	}
	pin := donorCalibrationEvidence{ref: calibrationRef, raw: appendOptionalBytes(calibrationRaw), modelLockSHA: modelLockSHA, datasetSHA: datasetSHA}
	edge, err := v2calibration.CalibrationMinFaceEdge(pin.raw, pin.ref.SHA256, pin.modelLockSHA, pin.datasetSHA)
	if err != nil {
		return zero, err
	}
	prepared, err := s.PrepareLoadedDonorInputEvidence(ctx, who, tenant, loaded, donorCatalog, manual, float64(edge))
	if err != nil {
		return zero, err
	}
	for _, entry := range prepared.Entries {
		if entry.Ref.ArtifactID == pin.ref.ArtifactID {
			return zero, v2input.ErrInvalid
		}
	}
	entries, err := uniqueTaskInputs(append(prepared.Entries, v2input.Entry{Ref: pin.ref, Contract: "Calibration"}))
	if err != nil {
		return zero, err
	}
	if len(entries) > 126 {
		return zero, v2input.ErrInvalid
	}
	prepared.Entries, prepared.calibration = entries, &pin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return prepared, nil
}
