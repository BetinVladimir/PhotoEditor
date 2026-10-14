package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Catalog identity is separate from verification of report links and pixel bytes.
type nativeDefectResultCatalog struct {
	ResultID uuid.UUID
	Outputs  [5]v2input.Ref
	Params   v2input.Ref
}

func (s SQLTransferSourceReader) nativeDefectResultCatalog(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, taskID uuid.UUID) (nativeDefectResultCatalog, error) {
	var zero nativeDefectResultCatalog
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || taskID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	var result nativeDefectResultCatalog
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outputs, params []byte
		if err := tx.QueryRow(ctx, "SELECT result_id,outputs,params FROM photo.semantic_defect_native_result_catalog($1,$2,$3,$4,$5,$6,$7,$8)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, taskID).Scan(&result.ResultID, &outputs, &params); err != nil {
			return err
		}
		var refs []v2input.Ref
		if json.Unmarshal(outputs, &refs) != nil || len(refs) != 5 || json.Unmarshal(params, &result.Params) != nil || result.ResultID == uuid.Nil {
			return v2input.ErrInvalid
		}
		seen := map[uuid.UUID]bool{}
		for _, ref := range append(refs, result.Params) {
			if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
				return v2input.ErrInvalid
			}
			if _, err := domain.ParseDigest(ref.SHA256); err != nil {
				return v2input.ErrInvalid
			}
			seen[ref.ArtifactID] = true
		}
		copy(result.Outputs[:], refs)
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrConflict
	}
	if err != nil {
		return zero, err
	}
	return result, nil
}

type VerifiedNativeDefectResult struct {
	TaskID, ResultID uuid.UUID
	Pins             DefectOriginPins
	Evidence         VerifiedDefectEvidence
	Faces            []review.Face68
	MaskRoots        []review.ProposalFaceMaskRoot
	admissionProof   nativeDefectAdmissionProof
	parentProof      nativeDefectParentProof
}

// Resolves only the current canonical native result, verifies original document
// links, physical masks, canonical landmark producers and original mask roots,
// then repeats catalog authority after all object IO. Explicit manual raster
// budgets are required when a manual root is declared.
// This does not approve candidates or advance a parent workflow.
func (s SQLTransferSourceReader) ReadNativeDefectResultEvidence(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, taskID uuid.UUID) (VerifiedNativeDefectResult, error) {
	var zero VerifiedNativeDefectResult
	before, err := s.nativeDefectResultCatalog(ctx, who, tenantID, binding, taskID)
	if err != nil {
		return zero, err
	}
	pins := DefectOriginPins{Labels: before.Outputs[0], Protection: before.Outputs[1], Proposal: before.Outputs[3], Report: before.Outputs[4], Params: before.Params}
	origin, err := s.readDefectOriginPreparationProof(ctx, who, tenantID, binding, pins)
	if err != nil {
		return zero, err
	}
	png, err := s.defectLabelPNGObject(ctx, who, tenantID, binding, pins, before.Outputs[2])
	if err != nil {
		return zero, err
	}
	evidence, err := s.ReadDefectEvidence(ctx, who, tenantID, binding, pins)
	if err != nil {
		return zero, err
	}
	if evidence.LabelPNG != before.Outputs[2] {
		return zero, v2input.ErrInvalid
	}
	faces, err := s.ReadDefectFaces(ctx, who, tenantID, binding, pins)
	if err != nil {
		return zero, err
	}
	maskRoots, err := s.ReadDefectFaceMaskRoots(ctx, who, tenantID, binding, pins)
	if err != nil {
		return zero, err
	}
	if err := s.recheckDefectOriginPreparationProof(ctx, who, tenantID, binding, origin); err != nil {
		return zero, err
	}
	currentPNG, err := s.defectLabelPNGObject(ctx, who, tenantID, binding, pins, before.Outputs[2])
	if err != nil {
		return zero, err
	}
	if currentPNG != png {
		return zero, v2input.ErrConflict
	}
	after, err := s.nativeDefectResultCatalog(ctx, who, tenantID, binding, taskID)
	if err != nil {
		return zero, err
	}
	if before != after {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return VerifiedNativeDefectResult{TaskID: taskID, ResultID: before.ResultID, Pins: pins, Evidence: evidence, Faces: faces, MaskRoots: maskRoots, admissionProof: nativeDefectAdmissionProof{task: taskID, catalog: before, origin: origin, png: png}}, nil
}
