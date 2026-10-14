package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

// Scheduler reads sensor metadata only through an import-source-scoped SQL
// function. It has no user session and must not use editor ACL impersonation.
type SQLSchedulerImportDustSensorReader struct {
	DB       *pgxpool.Pool
	Tenant   uuid.UUID
	Workflow uuid.UUID
	Parsed   []byte
}

func (r SQLSchedulerImportDustSensorReader) ReadImportDustSensor(ctx context.Context, binding v2input.SessionVersionBinding) (workflow.SensorAlignment, error) {
	var zero workflow.SensorAlignment
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if r.DB == nil || r.Tenant == uuid.Nil || r.Workflow == uuid.Nil || binding.AssetVersionID == uuid.Nil || !validImportRef(binding.Source) || !validImportRefForSHA(binding.CanvasHash) || len(r.Parsed) < 2 || len(r.Parsed) > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	parsed := append([]byte(nil), r.Parsed...)
	normalized, err := canonical.Bytes(parsed)
	if err != nil || !bytes.Equal(normalized, parsed) {
		return zero, v2input.ErrInvalid
	}
	var raw []byte
	err = database.TenantTransaction(ctx, r.DB, r.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.v30_import_dust_sensor_metadata($1,$2,$3,$4,$5,$6)`, r.Tenant, r.Workflow, string(parsed), binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256).Scan(&raw)
	})
	if err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 1024 || bytes.Equal(raw, []byte("null")) {
		return zero, v2input.ErrInvalid
	}
	var sensor workflow.SensorAlignment
	if json.Unmarshal(raw, &sensor) != nil || len(sensor.CameraID) != 68 || !strings.HasPrefix(sensor.CameraID, "raw-") || !validImportRefForSHA(sensor.CameraID[4:]) || sensor.Orientation != 1 || sensor.SensorWidth < 8 || sensor.SensorHeight < 8 || sensor.CropWidth < 8 || sensor.CropHeight < 8 || sensor.CropX < 0 || sensor.CropY < 0 || sensor.CropX+sensor.CropWidth > sensor.SensorWidth || sensor.CropY+sensor.CropHeight > sensor.SensorHeight {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return sensor, nil
}

var _ ImportDustSensorMetadataReader = SQLSchedulerImportDustSensorReader{}
