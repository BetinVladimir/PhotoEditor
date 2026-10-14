package interchange

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// QualifyExportParamsMask binds an inline MaskRef occurrence to a completed
// producer's sealed source and mask inputs under the export claim.
func (s SQLSchedulerExportInputCatalog) QualifyExportParamsMask(ctx context.Context,
	parent v2input.Entry, raw []byte, mask v2input.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || !validImportRef(mask) {
		return v2input.ErrInvalid
	}
	masks, err := exportParamsInlineMasks(parent, raw)
	if err != nil {
		return err
	}
	found := false
	for _, body := range masks {
		parsed, err := review.ParseMaskRef(body)
		if err != nil {
			return err
		}
		if parsed.Artifact.ID == mask.ArtifactID && parsed.Artifact.SHA256 == mask.SHA256 {
			found = true
		}
	}
	if !found {
		return v2input.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return v2input.ErrInvalid
	}
	operation, err := exportParamsOperationShape(raw)
	if err != nil || operation == "" {
		return v2input.ErrInvalid
	}
	var source v2input.Ref
	if json.Unmarshal(fields["image"], &source) != nil || !validImportRef(source) {
		return v2input.ErrInvalid
	}
	sourceEntry := v2input.Entry{Ref: source, Contract: "WorkingImage"}
	maskEntry := v2input.Entry{Ref: mask, Contract: "MaskPixels"}
	beforeParams, err := s.LookupExportDocument(ctx, parent)
	if err != nil {
		return err
	}
	beforeSource, err := s.LookupExportDocument(ctx, sourceEntry)
	if err != nil {
		return err
	}
	beforeMask, err := s.LookupExportDocument(ctx, maskEntry)
	if err != nil {
		return err
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_params_mask(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task, s.Claim.Result,
		s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA, s.Claim.Token,
		parent.Ref.ArtifactID, parent.Ref.SHA256, operation,
		source.ArtifactID, source.SHA256, mask.ArtifactID, mask.SHA256).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return v2input.ErrInvalid
	}
	afterParams, err := s.LookupExportDocument(ctx, parent)
	if err != nil || afterParams != beforeParams {
		return v2input.ErrInvalid
	}
	afterSource, err := s.LookupExportDocument(ctx, sourceEntry)
	if err != nil || afterSource != beforeSource {
		return v2input.ErrInvalid
	}
	afterMask, err := s.LookupExportDocument(ctx, maskEntry)
	if err != nil || afterMask != beforeMask {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportParamsMaskAuthority = SQLSchedulerExportInputCatalog{}
