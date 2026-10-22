package contactsheet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
)

type Service struct {
	DB         *pgxpool.Pool
	Resources  ResourceManager
	SRGBSHA256 string
}
type CreateRequest struct {
	SelectionSnapshotID uuid.UUID `json:"selection_snapshot_id"`
	Locale              string    `json:"locale"`
	Columns             int       `json:"columns"`
	Rows                int       `json:"rows"`
}
type Accepted struct {
	RunID  uuid.UUID `json:"run_id"`
	Status string    `json:"status"`
}
type previewEntry struct {
	Preview   artifactjson.Ref `json:"preview"`
	MediaType string           `json:"media_type"`
	Label     string           `json:"label"`
}
type params struct {
	Entries        []previewEntry   `json:"entries"`
	Locale         string           `json:"locale"`
	Font           artifactjson.Ref `json:"font"`
	FontLockID     string           `json:"font_lock_id"`
	Columns        int              `json:"columns"`
	Rows           int              `json:"rows"`
	CellWidth      int              `json:"cell_width"`
	CellHeight     int              `json:"cell_height"`
	PageBackground string           `json:"page_background"`
}

func normalize(r CreateRequest) (CreateRequest, error) {
	r.Locale = strings.ToLower(strings.TrimSpace(r.Locale))
	if r.SelectionSnapshotID == uuid.Nil || (r.Locale != "ru" && r.Locale != "bg" && r.Locale != "en") || r.Columns < 1 || r.Columns > 8 || r.Rows < 1 || r.Rows > 10 {
		return r, domain.NewError("VALIDATION_FAILED", 422)
	}
	return r, nil
}
func safeLabel(v string, ordinal int) string {
	v = strings.TrimSpace(v)
	if v == "" {
		v = fmt.Sprintf("%04d", ordinal+1)
	}
	r := []rune(v)
	if len(r) > 160 {
		v = string(r[:160])
	}
	return v
}

func resourceError(err error) error {
	switch {
	case errors.Is(err, artifactjson.ErrQuota):
		return domain.NewError("QUOTA_EXCEEDED", 422)
	case errors.Is(err, artifactjson.ErrConflict):
		return domain.NewError("CONTACT_SHEET_RESOURCE_CONFLICT", 409)
	case errors.Is(err, artifactjson.ErrStale):
		return domain.NewError("CONTACT_SHEET_RESOURCE_STALE", 409)
	default:
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
}

func (s Service) Create(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, raw CreateRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if err := ctx.Err(); err != nil { return out, err }
	r, e := normalize(raw)
	if e != nil {
		return out, e
	}
	if key == "" {
		return out, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	if replay, found, err := s.replay(ctx, who, tid, key, r); err != nil {
		return out, err
	} else if found {
		return replay, nil
	}
	if s.Resources.DB==nil||s.Resources.Objects==nil||s.Resources.Bucket==""{return out,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
	font, e := s.Resources.EnsureFont(ctx, who, tid)
	if e != nil {
		return out, resourceError(e)
	}
	profileHash, e := media.ProfileHash("preview1600", s.SRGBSHA256)
	if e != nil {
		return out, errors.New("contact sheet media profile unavailable")
	}
	e = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		var itemCount int
		x := tx.QueryRow(ctx, `SELECT c.project_id,s.item_count FROM photo.snapshots s JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id AND c.deleted_at IS NULL WHERE s.tenant_id=$1 AND s.id=$2 AND s.kind='selection' AND s.principal_type='user' AND s.principal_id=$3`, tid, r.SelectionSnapshotID, who.UserID).Scan(&projectID, &itemCount)
		if errors.Is(x, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if x != nil {
			return x
		}
		if _, x = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false); x != nil {
			return x
		}
		if itemCount < 1 || itemCount > r.Columns*r.Rows*128 {
			return domain.NewError("CONTACT_SHEET_PAGE_LIMIT", 422)
		}
		out, x = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "ContactSheet.Create", Key: key}, r, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			// Expiry prevents a fresh job, not replay of an already accepted one.
			var live bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.snapshots
			 WHERE tenant_id=$1 AND id=$2 AND kind='selection'
			 AND principal_type='user' AND principal_id=$3
			 AND (expires_at IS NULL OR expires_at>clock_timestamp()))`, tid,
				r.SelectionSnapshotID, who.UserID).Scan(&live); err != nil { return idempotency.Response{}, err }
			if !live { return idempotency.Response{}, domain.NewError("NOT_FOUND", 404) }
			rows, qerr := tx.Query(ctx, `SELECT si.ordinal,ar.id,ar.sha256,ar.media_type,coalesce(nullif(a.metadata->>'filename',''),'') FROM photo.snapshot_items si JOIN photo.asset_versions v ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id JOIN photo.derivatives d ON d.tenant_id=v.tenant_id AND d.asset_version_id=v.id AND d.variant='preview1600' AND d.profile_hash=$3 AND d.status='ready' JOIN photo.artifacts ar ON ar.tenant_id=d.tenant_id AND ar.id=d.artifact_id AND ar.state='ready' AND ar.object_version IS NOT NULL AND ar.media_type IN('image/jpeg','image/png','image/webp') AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp()) WHERE si.tenant_id=$1 AND si.snapshot_id=$2 ORDER BY si.ordinal`, tid, r.SelectionSnapshotID, profileHash)
			if qerr != nil {
				return idempotency.Response{}, qerr
			}
			defer rows.Close()
			entries := make([]previewEntry, 0, itemCount)
			inputs := make([]review.ArtifactRef, 0, itemCount+1)
			seen := make(map[uuid.UUID]string, itemCount)
			for rows.Next() {
				var ordinal int
				var entry previewEntry
				var label string
				if qerr = rows.Scan(&ordinal, &entry.Preview.ID, &entry.Preview.SHA256, &entry.MediaType, &label); qerr != nil {
					return idempotency.Response{}, qerr
				}
				entry.Label = safeLabel(label, ordinal)
				if previous, exists := seen[entry.Preview.ID]; exists {
					if previous != entry.Preview.SHA256 {
						return idempotency.Response{}, domain.NewError("ARTIFACT_IDENTITY_CONFLICT", 409)
					}
					return idempotency.Response{}, domain.NewError("CONTACT_SHEET_DUPLICATE_INPUT", 422)
				}
				seen[entry.Preview.ID] = entry.Preview.SHA256
				entries = append(entries, entry)
				inputs = append(inputs, review.ArtifactRef{ID: entry.Preview.ID, SHA256: entry.Preview.SHA256})
			}
			if qerr = rows.Err(); qerr != nil {
				return idempotency.Response{}, qerr
			}
			if len(entries) != itemCount {
				return idempotency.Response{}, domain.NewError("MEDIA_NOT_READY", 409)
			}
			inputs = append(inputs, review.ArtifactRef{ID: font.ID, SHA256: font.SHA256})
			p := params{entries, r.Locale, font, font.SHA256, r.Columns, r.Rows, 320, 260, "white"}
			praw, qerr := canonical.Marshal(p)
			if qerr != nil {
				return idempotency.Response{}, qerr
			}
			phash, qerr := canonical.Hash(p)
			if qerr != nil {
				return idempotency.Response{}, qerr
			}
			declared, qerr := canonical.Marshal(inputs)
			if qerr != nil {
				return idempotency.Response{}, qerr
			}
			var worker bool
			qerr = tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=$1 AND w.enabled AND w.last_seen>clock_timestamp()-interval '30 seconds' AND w.resource_classes ? 'media' AND w.backends ? 'cpu' AND (w.qualified_operations->'report.contact_sheet') ? 'cpu')`, tid).Scan(&worker)
			if qerr != nil {
				return idempotency.Response{}, qerr
			}
			if !worker {
				return idempotency.Response{}, domain.NewError("CONTACT_SHEET_CAPABILITY_UNAVAILABLE", 409)
			}
			run, task := uuid.New(), uuid.New()
			cfg := map[string]any{"schema_version": 1, "output_kind": "contact_sheet", "selection_snapshot_id": r.SelectionSnapshotID, "locale": r.Locale, "columns": r.Columns, "rows": r.Rows, "font_lock_id": font.SHA256}
			cfgRaw, qerr := canonical.Marshal(cfg)
			if qerr != nil {
				return idempotency.Response{}, qerr
			}
			cfgHash, qerr := canonical.Hash(cfg)
			if qerr != nil {
				return idempotency.Response{}, qerr
			}
			if _, qerr = tx.Exec(ctx, `INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES($1,$2,'export','queued',$3,$4,$5,$6,'fail_fast',false)`, run, tid, r.SelectionSnapshotID, cfgRaw, cfgHash, who.UserID); qerr != nil {
				return idempotency.Response{}, qerr
			}
			if _, qerr = tx.Exec(ctx, `INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,ordinal,operation,source_artifact_id,declared_inputs,params,params_sha256,backend,runtime_lock_id) VALUES($1,$2,$3,'contact_sheet',0,'report.contact_sheet',$4,$5,$6,$7,'cpu',NULL)`, tid, run, task, font.ID, declared, praw, phash); qerr != nil {
				return idempotency.Response{}, qerr
			}
			body, qerr := canonical.Marshal(Accepted{run, "queued"})
			return idempotency.Response{Status: 202, Body: body, ResourceID: &run}, qerr
		})
		if x!=nil{return x}
		if x=authorizeSelection(ctx,tx,who,tid,r);x!=nil{return x}
		if x=validateReceipt(ctx,tx,who,tid,key,r,out);x!=nil{return x}
		if isAbandoned(out,r,key){return domain.NewError("CONTACT_SHEET_ABANDONED",409)}
		if err:=authorizeSelection(ctx,tx,who,tid,r);err!=nil{return err}
		return ctx.Err()
	})
	return out, e
}
