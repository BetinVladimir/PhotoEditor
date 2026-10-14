package interchange

import (
	"context"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func (s SQLSchedulerExportInputCatalog) MatchExportBodyDependencyProfile(ctx context.Context,
	origin ExportBodyDependencyOrigin, parent v2input.Entry, raw []byte,
	child v2input.Entry, expected v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil ||
		!exportBodyDependencyQualificationOperation(expected.Operation) ||
		expected.Operation != origin.Producer.Operation || expected.Version != "1" ||
		!expected.LicenseCleared {
		return v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	before, err := s.LookupExportBodyDependencyOrigin(ctx, parent, body, child)
	if err != nil {
		return err
	}
	if before != origin {
		return v2input.ErrInvalid
	}
	policy, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_body_dependency_profile(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, child.Ref.ArtifactID, child.Ref.SHA256,
		origin.Producer.Task, origin.Producer.Attempt, origin.Producer.Fence,
		string(policy)).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return v2qualification.ErrUnqualified
	}
	after, err := s.LookupExportBodyDependencyOrigin(ctx, parent, body, child)
	if err != nil {
		return err
	}
	if after != origin {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportBodyDependencyQualificationReader = SQLSchedulerExportInputCatalog{}
