package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

type Service struct {
	DB     *pgxpool.Pool
	Pepper []byte
	Now    func() time.Time
}
type Request struct {
	SelectionSnapshotID uuid.UUID `json:"selection_snapshot_id"`
	PresetID            uuid.UUID `json:"preset_id"`
}
type Accepted struct {
	DownloadJobID uuid.UUID `json:"download_job_id"`
	Status        string    `json:"status"`
}
type Volume struct {
	Number       int    `json:"number"`
	Status       string `json:"status"`
	Bytes        int64  `json:"bytes"`
	DeliveryKind string `json:"delivery_kind"`
}
type Status struct {
	ID           uuid.UUID       `json:"id"`
	Status       string          `json:"status"`
	FilesDone    int64           `json:"files_done"`
	FilesTotal   int64           `json:"files_total"`
	BytesWritten int64           `json:"bytes_written"`
	Volumes      []Volume        `json:"volumes"`
	ExpiresAt    *time.Time      `json:"expires_at,omitempty"`
	Error        json.RawMessage `json:"error,omitempty"`
}
type IntentRequest struct {
	Volume int `json:"volume"`
}
type Intent struct {
	Token     string    `json:"intent_token"`
	ExpiresAt time.Time `json:"expires_at"`
}
type intentRecord struct {
	ID        uuid.UUID `json:"intent_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Service) Start(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req Request) (Accepted, error) {
	if req.SelectionSnapshotID == uuid.Nil || req.PresetID == uuid.Nil {
		return Accepted{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return Accepted{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	var result idempotency.Response
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var collectionID, projectID uuid.UUID
		var digest string
		var count int64
		var expires *time.Time
		if err := tx.QueryRow(ctx, `SELECT s.collection_id,s.digest,s.item_count,s.expires_at,c.project_id FROM photo.snapshots s JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id WHERE s.tenant_id=$1 AND s.id=$2 AND s.kind='selection' AND s.principal_type='user' AND s.principal_id=$3 AND s.item_count BETWEEN 1 AND 5000 FOR SHARE OF s,c`, tid, req.SelectionSnapshotID, who.UserID).Scan(&collectionID, &digest, &count, &expires, &projectID); errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("SNAPSHOT_STALE", 409)
		} else if err != nil {
			return err
		}
		if expires != nil && !s.now().Before(*expires) {
			return domain.NewError("SNAPSHOT_STALE", 409)
		}
		p, err := project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Viewer, false)
		if err != nil {
			return err
		}
		if !p.DownloadAllowed {
			return domain.NewError("DOWNLOAD_FORBIDDEN", 403)
		}
		var presetRevision int64
		var presetHash string
		var preset json.RawMessage
		if err := tx.QueryRow(ctx, `SELECT revision,config_hash,config FROM photo.export_presets WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL`, tid, req.PresetID).Scan(&presetRevision, &presetHash, &preset); errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		} else if err != nil {
			return err
		}
		return func() error {
			var err error
			result, err = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Download.Start", Key: key}, req, s.now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
				job, run := uuid.New(), uuid.New()
				cfg := map[string]any{"schema_version": 1, "pipeline": "download", "pipeline_version": 3, "input_snapshot_id": req.SelectionSnapshotID, "input_snapshot_digest": digest, "preset_id": req.PresetID, "preset_revision": presetRevision, "preset_hash": presetHash, "preset": preset, "failure_policy": "continue_items", "scope": "selected"}
				cfgHash, e := canonical.Hash(cfg)
				if e != nil {
					return idempotency.Response{}, e
				}
				if _, e = tx.Exec(ctx, `INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,$2,'download','queued',$3,$4,$5,$6)`, run, tid, req.SelectionSnapshotID, cfg, cfgHash, who.UserID); e != nil {
					return idempotency.Response{}, e
				}
				presetEnvelope := map[string]any{"id": req.PresetID, "revision": presetRevision, "hash": presetHash, "config": preset}
				if _, e = tx.Exec(ctx, `INSERT INTO photo.download_jobs(id,tenant_id,collection_id,principal_type,principal_id,snapshot_id,preset,status,run_id) VALUES($1,$2,$3,'user',$4,$5,$6,'queued',$7)`, job, tid, collectionID, who.UserID, req.SelectionSnapshotID, presetEnvelope, run); e != nil {
					return idempotency.Response{}, e
				}
				body, e := canonical.Marshal(Accepted{job, "queued"})
				return idempotency.Response{Status: 202, Body: body, ResourceID: &job}, e
			})
			return err
		}()
	})
	if err != nil {
		return Accepted{}, err
	}
	var out Accepted
	if err = json.Unmarshal(result.Body, &out); err != nil {
		return out, err
	}
	return out, nil
}

func scanStatus(row pgx.Row) (Status, error) {
	var out Status
	var volumes, failure []byte
	if err := row.Scan(&out.ID, &out.Status, &out.FilesDone, &out.FilesTotal, &out.BytesWritten, &volumes, &out.ExpiresAt, &failure); err != nil {
		return out, err
	}
	if err := json.Unmarshal(volumes, &out.Volumes); err != nil {
		return out, err
	}
	if out.FilesDone < 0 || out.FilesTotal < out.FilesDone || out.BytesWritten < 0 {
		return Status{}, errors.New("invalid download projection")
	}
	if len(failure) > 0 && string(failure) != "null" {
		out.Error = append([]byte(nil), failure...)
	}
	return out, nil
}
func statusRow(ctx context.Context, tx pgx.Tx, tid, user, id uuid.UUID) pgx.Row {
	return tx.QueryRow(ctx, `SELECT j.id,j.status,(SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id AND i.artifact_id IS NOT NULL),(SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id),coalesce((SELECT sum(v.bytes_written) FROM photo.download_volumes v WHERE v.tenant_id=j.tenant_id AND v.download_job_id=j.id),0),coalesce((SELECT jsonb_agg(jsonb_build_object('number',v.volume_no,'status',v.status,'bytes',v.bytes_written,'delivery_kind',coalesce((SELECT CASE WHEN bool_and(i.delivery_kind='single') THEN 'single' ELSE 'zip' END FROM photo.download_job_items i WHERE i.tenant_id=v.tenant_id AND i.download_job_id=v.download_job_id AND i.volume_no=v.volume_no),'zip')) ORDER BY v.volume_no) FROM photo.download_volumes v WHERE v.tenant_id=j.tenant_id AND v.download_job_id=j.id),'[]'::jsonb),j.expires_at,NULL::jsonb FROM photo.download_jobs j WHERE j.tenant_id=$1 AND j.id=$2 AND j.principal_type='user' AND j.principal_id=$3`, tid, id, user)
}
func authorizeJob(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, id uuid.UUID, role domain.Role, lock bool) (uuid.UUID, string, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF j"
	}
	var run, projectID uuid.UUID
	var status string
	err := tx.QueryRow(ctx, `SELECT j.run_id,j.status,c.project_id FROM photo.download_jobs j JOIN photo.collections c ON c.tenant_id=j.tenant_id AND c.id=j.collection_id WHERE j.tenant_id=$1 AND j.id=$2 AND j.principal_type='user' AND j.principal_id=$3`+suffix, tid, id, who.UserID).Scan(&run, &status, &projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return uuid.Nil, "", err
	}
	p, err := project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Viewer, false)
	if err != nil {
		return uuid.Nil, "", err
	}
	if !p.DownloadAllowed {
		return uuid.Nil, "", domain.NewError("DOWNLOAD_FORBIDDEN", 403)
	}
	return run, status, nil
}
func (s Service) Get(ctx context.Context, who auth.Identity, tid, id uuid.UUID) (Status, error) {
	var out Status
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		if _, _, e := authorizeJob(ctx, tx, who, tid, id, role, false); e != nil {
			return e
		}
		var e error
		out, e = scanStatus(statusRow(ctx, tx, tid, who.UserID, id))
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		return e
	})
	return out, err
}
func (s Service) Cancel(ctx context.Context, who auth.Identity, tid, id uuid.UUID) (Status, error) {
	var out Status
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		run, status, e := authorizeJob(ctx, tx, who, tid, id, role, true)
		if e != nil {
			return e
		}
		if status == "queued" || status == "preparing" || status == "building" || status == "uploading" {
			if _, e := tx.Exec(ctx, `UPDATE photo.download_jobs SET cancel_requested=true,status='cancelling' WHERE tenant_id=$1 AND id=$2`, tid, id); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, `UPDATE photo.runs SET cancel_requested=true,status=CASE WHEN status IN ('draft','queued','running','waiting_review') THEN 'cancelling' ELSE status END,revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, run); e != nil {
				return e
			}
		}
		out, e = scanStatus(statusRow(ctx, tx, tid, who.UserID, id))
		return e
	})
	return out, err
}
func (s Service) CreateIntent(ctx context.Context, who auth.Identity, tid, job uuid.UUID, key string, req IntentRequest) (Intent, error) {
	if job == uuid.Nil || req.Volume < 1 || req.Volume > 5000 {
		return Intent{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return Intent{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	var response idempotency.Response
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		if _, _, e := authorizeJob(ctx, tx, who, tid, job, role, false); e != nil {
			return e
		}
		var stale bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.download_jobs j JOIN photo.snapshot_items si ON si.tenant_id=j.tenant_id AND si.snapshot_id=j.snapshot_id LEFT JOIN photo.collection_items ci ON ci.tenant_id=si.tenant_id AND ci.collection_id=j.collection_id AND ci.id=si.item_id AND ci.asset_version_id=si.asset_version_id WHERE j.tenant_id=$1 AND j.id=$2 AND (ci.id IS NULL))`, tid, job).Scan(&stale); e != nil {
			return e
		}
		if stale {
			return domain.NewError("DOWNLOAD_REVOKED", 409)
		}
		return func() error {
			var e error
			response, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Download.Intent", Key: key}, struct {
				Job    uuid.UUID `json:"download_job_id"`
				Volume int       `json:"volume"`
			}{job, req.Volume}, s.now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
				var artifact uuid.UUID
				var jobExpires time.Time
				if e := tx.QueryRow(ctx, `SELECT v.artifact_id,j.expires_at FROM photo.download_jobs j JOIN photo.download_volumes v ON v.tenant_id=j.tenant_id AND v.download_job_id=j.id AND v.volume_no=$4 AND v.status='ready' JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.artifact_id AND a.state='ready' AND a.expires_at>clock_timestamp() AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null') WHERE j.tenant_id=$1 AND j.id=$2 AND j.principal_type='user' AND j.principal_id=$3 AND j.status='ready' AND NOT j.cancel_requested AND j.expires_at>clock_timestamp()`, tid, job, who.UserID, req.Volume).Scan(&artifact, &jobExpires); errors.Is(e, pgx.ErrNoRows) {
					return idempotency.Response{}, domain.NewError("DOWNLOAD_NOT_READY", 409)
				} else if e != nil {
					return idempotency.Response{}, e
				}
				id := uuid.New()
				token := base64.RawURLEncoding.EncodeToString(auth.MAC(s.Pepper, "portal-download-bearer", id.String()))
				expires := s.now().Add(time.Minute)
				if jobExpires.Before(expires) {
					expires = jobExpires
				}
				if _, e = tx.Exec(ctx, `INSERT INTO photo.download_intents(id,tenant_id,principal_type,principal_id,artifact_id,token_hash,expires_at) VALUES($1,$2,'user',$3,$4,$5,$6)`, id, tid, who.UserID, artifact, auth.MAC(s.Pepper, "portal-download-intent", token), expires); e != nil {
					return idempotency.Response{}, e
				}
				body, e := canonical.Marshal(intentRecord{id, expires})
				return idempotency.Response{Status: 201, Body: body, ResourceID: &id}, e
			})
			return e
		}()
	})
	if err != nil {
		return Intent{}, err
	}
	var saved intentRecord
	if err = json.Unmarshal(response.Body, &saved); err != nil || saved.ID == uuid.Nil {
		return Intent{}, errors.New("invalid download intent replay")
	}
	token := base64.RawURLEncoding.EncodeToString(auth.MAC(s.Pepper, "portal-download-bearer", saved.ID.String()))
	return Intent{Token: token, ExpiresAt: saved.ExpiresAt}, nil
}
