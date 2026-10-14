package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Ordered original children: protected NPY, target PNG, protected PNG.
type defectManualOutputs [3]defectEvidenceObject

func (s SQLTransferSourceReader) defectManualOutputObjects(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, pixel v2input.Ref, refs [3]v2input.Ref) (defectManualOutputs, error) {
	empty := defectManualOutputs{}
	if _, err := s.defectFaceMaskObjects(ctx, who, tenantID, binding, pins, pixel, "manual"); err != nil {
		return empty, err
	}
	seen := map[uuid.UUID]bool{pixel.ArtifactID: true}
	for _, ref := range refs {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return empty, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return empty, err
		}
		seen[ref.ArtifactID] = true
	}
	var result defectManualOutputs
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		for i, role := range []string{"mask_protected", "mask_target_png", "mask_protected_png"} {
			entry := defectEvidenceObject{ref: refs[i]}
			var bucket string
			err := tx.QueryRow(ctx, "SELECT bucket,object_key,object_version,bytes,media_type FROM photo.semantic_proposal_defect_manual_output_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256, pixel.ArtifactID, pixel.SHA256, "manual", refs[i].ArtifactID, refs[i].SHA256, role).Scan(&bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes, &entry.object.ContentType)
			if err != nil {
				return err
			}
			media, min, max := "image/png", int64(57), int64(613048576)
			if i == 0 {
				media, min, max = "application/octet-stream", 32, 800065548
			}
			if bucket != s.Bucket || entry.object.Key == "" || entry.object.VersionID == "" || entry.object.VersionID == "null" || entry.object.ContentType != media || entry.object.Bytes < min || entry.object.Bytes > max {
				return v2input.ErrInvalid
			}
			entry.object.SHA256 = refs[i].SHA256
			result[i] = entry
		}
		return nil
	})
	if err != nil {
		return empty, err
	}
	return result, nil
}

func (s SQLTransferSourceReader) verifyManualProtectionCoverage(ctx context.Context, required, protection defectEvidenceObject, width, height int64) error {
	left, err := s.Objects.Open(ctx, required.object)
	if err != nil {
		return err
	}
	right, err := s.Objects.Open(ctx, protection.object)
	if err != nil {
		_ = left.Close()
		return err
	}
	_, verifyErr := masknpy.VerifyProbabilityMaskCoverage(ctx, masknpy.PinnedMask{Reader: left, Bytes: required.object.Bytes, SHA256: required.ref.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: protection.object.Bytes, SHA256: protection.ref.SHA256}, width, height)
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr != nil {
		return verifyErr
	}
	if leftErr != nil {
		return leftErr
	}
	return rightErr
}
