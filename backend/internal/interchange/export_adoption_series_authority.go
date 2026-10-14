package interchange

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

// QualifyExportSeries checks the completed producer under the same scheduler
// claim that supplied the object's immutable version. The native validator
// separately checks the SeriesCorrections bytes and their input semantics.
func (s SQLSchedulerExportInputCatalog) QualifyExportSeries(ctx context.Context, entry v2input.Entry, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || entry.Contract != "SeriesCorrections" ||
		!validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != entry.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	object, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return err
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_series($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, entry.Ref.ArtifactID, entry.Ref.SHA256,
		s.Bucket, object.Key, object.VersionID, object.Bytes).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return v2input.ErrInvalid
	}
	current, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return err
	}
	if current != object {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportSeriesAuthority = SQLSchedulerExportInputCatalog{}
