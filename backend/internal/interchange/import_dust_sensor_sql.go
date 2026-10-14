package interchange

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

type SQLImportDustSensorReader struct {
	DB     *pgxpool.Pool
	Who    auth.Identity
	Tenant uuid.UUID
}

func (r SQLImportDustSensorReader) ReadImportDustSensor(ctx context.Context, binding v2input.SessionVersionBinding) (workflow.SensorAlignment, error) {
	var sensor workflow.SensorAlignment
	if err := ctx.Err(); err != nil {
		return sensor, err
	}
	if r.DB == nil || r.Tenant == uuid.Nil || r.Who.UserID == uuid.Nil || r.Who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || !validImportRef(binding.Source) || !validImportRefForSHA(binding.CanvasHash) {
		return sensor, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, r.DB, r.Who, r.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT v.metadata->'ingest'->'sensor_alignment'
		 FROM photo.asset_versions v
		 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
		 JOIN photo.projects p ON p.tenant_id=a.tenant_id AND p.id=a.project_id
		 JOIN photo.tenants t ON t.id=p.tenant_id
		 WHERE v.tenant_id=$1 AND v.id=$2 AND v.master_artifact_id=$3 AND v.content_hash=$4
		 AND v.review_state='accepted' AND a.deleted_at IS NULL AND a.upload_state='verified'
		 AND p.archived_at IS NULL AND t.status='active'
		 AND v.metadata#>>'{ingest,format}'='raw'
		 AND jsonb_typeof(v.metadata->'ingest'->'sensor_alignment')='object'
		 AND (SELECT count(*) FROM jsonb_object_keys(v.metadata->'ingest'->'sensor_alignment'))=8`,
			r.Tenant, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256).Scan(&raw)
		if err != nil {
			return err
		}
		if len(raw) < 2 || len(raw) > 1024 || json.Unmarshal(raw, &sensor) != nil || len(sensor.CameraID) != 68 || !strings.HasPrefix(sensor.CameraID, "raw-") || !validImportRefForSHA(sensor.CameraID[4:]) || sensor.Orientation != 1 || sensor.SensorWidth < 8 || sensor.SensorHeight < 8 || sensor.CropWidth < 8 || sensor.CropHeight < 8 || sensor.CropX < 0 || sensor.CropY < 0 || sensor.CropX+sensor.CropWidth > sensor.SensorWidth || sensor.CropY+sensor.CropHeight > sensor.SensorHeight {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return workflow.SensorAlignment{}, err
	}
	return sensor, ctx.Err()
}

var _ ImportDustSensorMetadataReader = SQLImportDustSensorReader{}
