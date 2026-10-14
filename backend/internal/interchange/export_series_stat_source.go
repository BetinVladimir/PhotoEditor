package interchange

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type ExportSeriesStatImageAuthority interface {
	QualifyExportSeriesStatImage(context.Context, ExportSeriesOriginEvidence, v2input.Entry, []byte) error
}
type ExportSeriesStatInputs struct {
	Authority ExportSeriesStatImageAuthority
	Images    ExportClosureNodeResolver
	Warps     ExportDenseWarpOccurrenceAuthority
	Measure   ExportSeriesMeasureAuthority
}

func (s ExportSeriesStatInputs) QualifyExportSeriesInputs(ctx context.Context, origin ExportSeriesOriginEvidence, bundle []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Authority == nil || s.Images == nil || s.Measure == nil || len(bundle) < 2 || len(bundle) > 32<<20 {
		return v2input.ErrInvalid
	}
	var document exportSeriesOriginBundle
	if json.Unmarshal(bundle, &document) != nil || len(document.Stats) < 2 || len(document.Stats) > 2000 {
		return v2input.ErrInvalid
	}
	for _, stat := range document.Stats {
		entry := v2input.Entry{Ref: v2input.Ref{ArtifactID: stat.ID, SHA256: stat.SHA}, Contract: "SeriesStats"}
		if !validImportRef(entry.Ref) || rawSHA(stat.Body) != stat.SHA {
			return v2input.ErrInvalid
		}
		var value struct {
			Image v2input.Ref `json:"image"`
		}
		if json.Unmarshal(stat.Body, &value) != nil || !validImportRef(value.Image) {
			return v2input.ErrInvalid
		}
		if err := s.Authority.QualifyExportSeriesStatImage(ctx, origin, entry, append([]byte(nil), stat.Body...)); err != nil {
			return err
		}
		expected := v2input.Entry{Ref: value.Image, Contract: "WorkingImage"}
		node, err := s.Images.ResolveExportNode(ctx, expected)
		if err != nil {
			return err
		}
		if node.Entry != expected || rawSHA(node.Body) != value.Image.SHA256 {
			return v2input.ErrInvalid
		}
		image, err := review.ParseWorkingImage(node.Body)
		if err != nil {
			return err
		}
		if image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" {
			return v2input.ErrInvalid
		}
		if err := qualifyExportResolvedSourceWarps(ctx, node, s.Warps); err != nil {
			return err
		}
		if err := s.Measure.QualifyExportSeriesMeasure(ctx, origin, entry, append([]byte(nil), stat.Body...), node); err != nil {
			return err
		}
		if err := s.Authority.QualifyExportSeriesStatImage(ctx, origin, entry, append([]byte(nil), stat.Body...)); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s SQLExportDocumentCatalog) QualifyExportSeriesStatImage(ctx context.Context, origin ExportSeriesOriginEvidence, entry v2input.Entry, raw []byte) error {
	return qualifyExportSeriesStatImage(ctx, s, s.DB, s.Tenant, s.Who, s.Request.ProjectID, origin, entry, raw)
}
func (s SQLExportResultInputCatalog) QualifyExportSeriesStatImage(ctx context.Context, origin ExportSeriesOriginEvidence, entry v2input.Entry, raw []byte) error {
	config := s.Export
	return qualifyExportSeriesStatImage(ctx, s, config.DB, config.Tenant, config.Who, config.Request.ProjectID, origin, entry, raw)
}
func qualifyExportSeriesStatImage(ctx context.Context, source ExportSeriesOriginSource, db *pgxpool.Pool, tenantID uuid.UUID, who auth.Identity, project uuid.UUID, origin ExportSeriesOriginEvidence, entry v2input.Entry, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if source == nil || db == nil || tenantID == uuid.Nil || project == uuid.Nil || entry.Contract != "SeriesStats" || !validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != entry.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	var value struct {
		Image v2input.Ref `json:"image"`
	}
	if json.Unmarshal(raw, &value) != nil || !validImportRef(value.Image) {
		return v2input.ErrInvalid
	}
	object, err := source.LookupExportSeriesOriginDocument(ctx, origin, entry)
	if err != nil {
		return err
	}
	err = tenant.WithTenant(ctx, db, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.artifacts stats
   JOIN photo.tasks measure ON measure.tenant_id=stats.tenant_id AND measure.id=stats.created_by_task_id
   JOIN photo.task_results result ON result.tenant_id=measure.tenant_id AND result.id=measure.canonical_result_id AND result.task_id=measure.id AND result.attempt=measure.attempt AND result.fencing_token=measure.fencing_token
   WHERE stats.tenant_id=$1 AND stats.id=$2 AND stats.sha256=$3 AND stats.produced_attempt=measure.attempt AND stats.produced_fence=measure.fencing_token
   AND measure.operation='series.measure' AND measure.operation_version='1' AND measure.state='succeeded'
   AND measure.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$4::uuid,'sha256',$5::text))
   AND photo.v2_editor_authorized($1,$6,$7,$8)
   AND EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
    WHERE v.tenant_id=$1 AND a.project_id=$6 AND v.master_artifact_id=$4 AND v.content_hash=$5 AND v.review_state='accepted' AND a.deleted_at IS NULL AND a.upload_state='verified'))`, tenantID, entry.Ref.ArtifactID, entry.Ref.SHA256, value.Image.ArtifactID, value.Image.SHA256, project, who.UserID, who.SessionID).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return err
	}
	current, err := source.LookupExportSeriesOriginDocument(ctx, origin, entry)
	if err != nil {
		return err
	}
	if current != object {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportSeriesStatImageAuthority = SQLExportDocumentCatalog{}
var _ ExportSeriesStatImageAuthority = SQLExportResultInputCatalog{}
