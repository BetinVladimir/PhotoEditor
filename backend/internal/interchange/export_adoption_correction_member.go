package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) QualifyExportCorrectionMember(ctx context.Context,
	binding v2input.SessionVersionBinding, parent v2input.Ref, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil ||
		binding.AssetVersionID == uuid.Nil || !validImportRef(binding.Source) ||
		!validImportRef(parent) || parent.ArtifactID == binding.Source.ArtifactID ||
		len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != binding.Source.SHA256 {
		return v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		return err
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != binding.CanvasHash {
		return v2input.ErrInvalid
	}
	source := v2input.Entry{Ref: binding.Source, Contract: "WorkingImage"}
	root := v2input.Entry{Ref: parent, Contract: "CorrectionPlan"}
	object, err := s.LookupExportDocument(ctx, source)
	if err != nil {
		return err
	}
	parentObject, err := s.LookupExportDocument(ctx, root)
	if err != nil {
		return err
	}
	if object.Bytes != int64(len(raw)) {
		return v2input.ErrInvalid
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_correction_member(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, binding.Source.ArtifactID, binding.Source.SHA256,
		parent.ArtifactID, parent.SHA256, binding.AssetVersionID,
		s.Bucket, object.Key, object.VersionID, object.Bytes).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return v2input.ErrInvalid
	}
	current, err := s.LookupExportDocument(ctx, source)
	if err != nil {
		return err
	}
	currentParent, err := s.LookupExportDocument(ctx, root)
	if err != nil {
		return err
	}
	if current != object || currentParent != parentObject {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportCorrectionMemberAuthority = SQLSchedulerExportInputCatalog{}
