package exportjob

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type Service struct{ DB *pgxpool.Pool }
type CreateRequest struct {
	SnapshotID uuid.UUID   `json:"snapshot_id"`
	Presets    []uuid.UUID `json:"presets"`
}
type RunAccepted struct {
	RunID  uuid.UUID `json:"run_id"`
	Status string    `json:"status"`
}

var terminalCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,79}$`)

func (s Service) Create(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req CreateRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if req.SnapshotID == uuid.Nil || len(req.Presets) < 1 || len(req.Presets) > 16 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range req.Presets {
		if id == uuid.Nil || seen[id] {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		seen[id] = true
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var collectionID, projectID uuid.UUID
		var digest string
		var count int64
		var expires *time.Time
		e := tx.QueryRow(ctx, `SELECT s.collection_id,s.digest,s.item_count,s.expires_at,c.project_id FROM photo.snapshots s JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id WHERE s.tenant_id=$1 AND s.id=$2 AND s.kind='selection' AND s.principal_type='user' AND s.principal_id=$3 AND s.item_count BETWEEN 1 AND 5000 FOR SHARE OF s,c`, tid, req.SnapshotID, who.UserID).Scan(&collectionID, &digest, &count, &expires, &projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("SNAPSHOT_STALE", 409)
		}
		if e != nil {
			return e
		}
		if expires != nil && !time.Now().UTC().Before(*expires) {
			return domain.NewError("SNAPSHOT_STALE", 409)
		}
		p, e := project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false)
		if e != nil {
			return e
		}
		if !p.DownloadAllowed {
			return domain.NewError("DOWNLOAD_FORBIDDEN", 403)
		}
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Export.Create", Key: key}, req, time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
			batch := uuid.New()
			cfg := map[string]any{"schema_version": 1, "pipeline": "export_batch", "input_snapshot_id": req.SnapshotID, "input_snapshot_digest": digest, "presets": req.Presets}
			hash, x := canonical.Hash(cfg)
			if x != nil {
				return idempotency.Response{}, x
			}
			if _, x = tx.Exec(ctx, `INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES($1,$2,'export_batch','running',$3,$4,$5,$6,'continue_items',true)`, batch, tid, req.SnapshotID, cfg, hash, who.UserID); x != nil {
				return idempotency.Response{}, x
			}
			if _, x = tx.Exec(ctx, `INSERT INTO photo.export_batches(tenant_id,run_id,preset_count) VALUES($1,$2,$3)`, tid, batch, len(req.Presets)); x != nil {
				return idempotency.Response{}, x
			}
			for _, presetID := range req.Presets {
				var revision int64
				var presetHash string
				var preset json.RawMessage
				x = tx.QueryRow(ctx, `SELECT revision,config_hash,config FROM photo.export_presets WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL`, tid, presetID).Scan(&revision, &presetHash, &preset)
				if errors.Is(x, pgx.ErrNoRows) {
					return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
				}
				if x != nil {
					return idempotency.Response{}, x
				}
				child, job := uuid.New(), uuid.New()
				childCfg := map[string]any{"schema_version": 1, "pipeline": "download", "pipeline_version": 3, "input_snapshot_id": req.SnapshotID, "input_snapshot_digest": digest, "preset_id": presetID, "preset_revision": revision, "preset_hash": presetHash, "preset": preset, "failure_policy": "continue_items", "scope": "selected"}
				childHash, x := canonical.Hash(childCfg)
				if x != nil {
					return idempotency.Response{}, x
				}
				if _, x = tx.Exec(ctx, `INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,$2,'download','queued',$3,$4,$5,$6)`, child, tid, req.SnapshotID, childCfg, childHash, who.UserID); x != nil {
					return idempotency.Response{}, x
				}
				envelope := map[string]any{"id": presetID, "revision": revision, "hash": presetHash, "config": preset}
				if _, x = tx.Exec(ctx, `INSERT INTO photo.download_jobs(id,tenant_id,collection_id,principal_type,principal_id,snapshot_id,preset,status,run_id) VALUES($1,$2,$3,'user',$4,$5,$6,'queued',$7)`, job, tid, collectionID, who.UserID, req.SnapshotID, envelope, child); x != nil {
					return idempotency.Response{}, x
				}
				if _, x = tx.Exec(ctx, `INSERT INTO photo.export_batch_children(tenant_id,batch_run_id,child_run_id,download_job_id,preset_id,preset_revision,preset_hash) VALUES($1,$2,$3,$4,$5,$6,$7)`, tid, batch, child, job, presetID, revision, presetHash); x != nil {
					return idempotency.Response{}, x
				}
			}
			body, x := canonical.Marshal(RunAccepted{batch, "queued"})
			return idempotency.Response{Status: 202, Body: body, ResourceID: &batch}, x
		})
		return e
	})
	return out, err
}

// Status deliberately excludes bucket, object key and object version. Delivery
// is available only through a separately authorized download intent.
type Status struct {
	ID             uuid.UUID       `json:"id"`
	AssetVersionID uuid.UUID       `json:"asset_version_id"`
	PresetHash     string          `json:"preset_hash"`
	Status         string          `json:"status"`
	ArtifactReady  bool            `json:"artifact_ready"`
	Bytes          *int64          `json:"bytes,omitempty"`
	MediaType      *string         `json:"media_type,omitempty"`
	Width          *int            `json:"width,omitempty"`
	Height         *int            `json:"height,omitempty"`
	Error          json.RawMessage `json:"error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

func (s Service) Get(ctx context.Context, who auth.Identity, tid, id uuid.UUID) (Status, error) {
	var out Status
	if s.DB == nil || tid == uuid.Nil || id == uuid.Nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		var terminal *string
		var artifactState *string
		err := tx.QueryRow(ctx, `SELECT e.id,e.asset_version_id,e.preset_hash,e.status,e.created_at,a.bytes,a.media_type,a.width,a.height,a.state,t.terminal_reason,source.project_id
		 FROM photo.exports e
		 JOIN photo.asset_versions v ON v.tenant_id=e.tenant_id AND v.id=e.asset_version_id
		 JOIN photo.assets source ON source.tenant_id=v.tenant_id AND source.id=v.asset_id AND source.deleted_at IS NULL
		 LEFT JOIN photo.artifacts a ON a.tenant_id=e.tenant_id AND a.id=e.artifact_id AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
		 LEFT JOIN photo.tasks t ON t.tenant_id=e.tenant_id AND t.id=e.task_id
		 WHERE e.tenant_id=$1 AND e.id=$2`, tid, id).Scan(&out.ID, &out.AssetVersionID, &out.PresetHash, &out.Status, &out.CreatedAt, &out.Bytes, &out.MediaType, &out.Width, &out.Height, &artifactState, &terminal, &projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if _, err = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Viewer, false); err != nil {
			return err
		}
		out.ArtifactReady = out.Status == "ready" && artifactState != nil && *artifactState == "ready"
		if !out.ArtifactReady {
			out.Bytes, out.MediaType, out.Width, out.Height = nil, nil, nil, nil
		}
		if terminal != nil && *terminal != "" {
			code := *terminal
			if !terminalCode.MatchString(code) {
				code = "EXPORT_FAILED"
			}
			encoded, _ := json.Marshal(map[string]string{"code": code})
			out.Error = encoded
		}
		return nil
	})
	return out, err
}
