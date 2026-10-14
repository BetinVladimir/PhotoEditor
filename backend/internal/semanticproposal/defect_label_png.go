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

func (s SQLTransferSourceReader) defectLabelPNGObject(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, png v2input.Ref) (defectEvidenceObject, error) {
	empty := defectEvidenceObject{}
	if s.Objects == nil || s.Bucket == "" || png.ArtifactID == uuid.Nil {
		return empty, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(png.SHA256); err != nil {
		return empty, v2input.ErrInvalid
	}
	for _, ref := range []v2input.Ref{pins.Params, pins.Proposal, pins.Report, pins.Labels, pins.Protection} {
		if ref.ArtifactID == png.ArtifactID {
			return empty, v2input.ErrInvalid
		}
	}
	if err := s.VerifyDefectOrigin(ctx, who, tenantID, binding, pins); err != nil {
		return empty, err
	}
	entry := defectEvidenceObject{ref: png}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, "SELECT bucket,object_key,object_version,bytes,media_type FROM photo.semantic_proposal_defect_label_png_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256, png.ArtifactID, png.SHA256).Scan(&bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes, &entry.object.ContentType)
		if err != nil {
			return err
		}
		if bucket != s.Bucket || entry.object.Key == "" || entry.object.VersionID == "" || entry.object.VersionID == "null" || entry.object.ContentType != "image/png" || entry.object.Bytes < 57 || entry.object.Bytes > 613048576 {
			return v2input.ErrInvalid
		}
		entry.object.SHA256 = png.SHA256
		return nil
	})
	if err != nil {
		return empty, err
	}
	return entry, nil
}

func (s SQLTransferSourceReader) verifyDefectLabelPNG(ctx context.Context, labels, png defectEvidenceObject, width, height int64) error {
	left, err := s.Objects.Open(ctx, labels.object)
	if err != nil {
		return err
	}
	right, err := s.Objects.Open(ctx, png.object)
	if err != nil {
		_ = left.Close()
		return err
	}
	verifyErr := masknpy.VerifyDefectLabelPNG(ctx, masknpy.PinnedMask{Reader: left, Bytes: labels.object.Bytes, SHA256: labels.ref.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: png.object.Bytes, SHA256: png.ref.SHA256}, width, height)
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr != nil {
		return verifyErr
	}
	if leftErr != nil {
		return leftErr
	}
	return rightErr
}
