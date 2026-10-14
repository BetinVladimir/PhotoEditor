package interchange

import (
	"context"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) QualifyExportBodyFitInputInventory(ctx context.Context,
	parent v2input.Entry, raw []byte, fit ExportClosureNode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil {
		return v2input.ErrInvalid
	}
	inventory, err := exportBodyFitInputInventory(fit)
	if err != nil {
		return err
	}
	body := append([]byte(nil), raw...)
	origin, err := s.LookupExportBodyDependencyOrigin(ctx, parent, body, fit.Entry)
	if err != nil {
		return err
	}
	if origin.Producer.Operation != "pair.fit.body" ||
		origin.Output.Bytes != int64(len(fit.Body)) {
		return v2input.ErrInvalid
	}
	inputs, err := canonical.Marshal(inventory.Inputs)
	if err != nil {
		return err
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_body_fit_inventory(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
 $17,$18::jsonb,$19,$20,$21,$22,$23,$24,$25,$26)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, parent.Ref.ArtifactID, parent.Ref.SHA256,
		fit.Entry.Ref.ArtifactID, fit.Entry.Ref.SHA256,
		origin.Producer.Task, origin.Producer.Attempt, origin.Producer.Fence,
		origin.Params.ArtifactID, string(inputs),
		inventory.Before.ArtifactID, inventory.Before.SHA256,
		inventory.After.ArtifactID, inventory.After.SHA256,
		s.Bucket, origin.Output.Key, origin.Output.VersionID,
		origin.Output.Bytes).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return v2input.ErrInvalid
	}
	current, err := s.LookupExportBodyDependencyOrigin(ctx, parent, body, fit.Entry)
	if err != nil {
		return err
	}
	if current != origin {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportBodyFitInventoryAuthority = SQLSchedulerExportInputCatalog{}
