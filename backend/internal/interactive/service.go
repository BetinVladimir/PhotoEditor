package interactive

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"time"
)

type Service struct{ DB *pgxpool.Pool }
type ArtifactRef struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
}
type MaskCreate struct {
	AssetVersionID uuid.UUID       `json:"asset_version_id"`
	InputSHA256    string          `json:"input_sha256"`
	ObjectID       *uuid.UUID      `json:"object_id,omitempty"`
	Selector       string          `json:"selector"`
	Strokes        json.RawMessage `json:"strokes,omitempty"`
	Polygons       json.RawMessage `json:"polygons,omitempty"`
	Points         json.RawMessage `json:"points,omitempty"`
	Box            json.RawMessage `json:"box,omitempty"`
	ProtectedRefs  json.RawMessage `json:"protected_refs,omitempty"`
}
type MaskAccepted struct {
	RunID        uuid.UUID `json:"run_id"`
	SourceSHA256 string    `json:"source_sha256"`
	Status       string    `json:"status"`
}
type GenerateRequest struct {
	AssetVersionID uuid.UUID       `json:"asset_version_id"`
	DraftRevision  int64           `json:"draft_revision"`
	MaskRef        json.RawMessage `json:"mask_ref"`
	ProtectedRefs  json.RawMessage `json:"protected_refs"`
	Params         json.RawMessage `json:"params"`
	VariantCount   int             `json:"variant_count"`
}
type PreviewRequest struct {
	AssetVersionID uuid.UUID       `json:"asset_version_id"`
	DraftRevision  int64           `json:"draft_revision"`
	StyleVersionID *uuid.UUID      `json:"style_version_id,omitempty"`
	Params         json.RawMessage `json:"params"`
	ROI            *[4]int         `json:"roi,omitempty"`
	Scale          string          `json:"scale"`
	Generation     bool            `json:"generation"`
}
type RunAccepted struct {
	RunID  uuid.UUID `json:"run_id"`
	Status string    `json:"status"`
}
type source struct {
	ProjectID, ArtifactID uuid.UUID
	SHA                   string
	Bytes                 int64
}

func loadSource(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, version uuid.UUID, role domain.Role) (source, error) {
	var v source
	e := tx.QueryRow(ctx, `SELECT a.project_id,v.master_artifact_id,ar.sha256,ar.bytes FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id AND a.deleted_at IS NULL JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id AND ar.sha256=v.content_hash AND ar.state='ready' AND ar.object_version IS NOT NULL AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp()) WHERE v.tenant_id=$1 AND v.id=$2`, tid, version).Scan(&v.ProjectID, &v.ArtifactID, &v.SHA, &v.Bytes)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, domain.NewError("NOT_FOUND", 404)
	}
	if e != nil {
		return v, e
	}
	// The source query can wait on a relation lock. Do not carry an earlier
	// administrator role across that wait into project authorization.
	role, e = tenant.AuthorizeInTransaction(ctx, tx, who, tid, domain.Viewer)
	if e != nil {
		return v, e
	}
	_, e = project.Authorize(ctx, tx, tid, v.ProjectID, who.UserID, role, domain.Editor, false)
	return v, e
}
type runtimeQuery interface { QueryRow(context.Context,string,...any) pgx.Row }
func runtime(ctx context.Context, tx runtimeQuery, tid, projectID uuid.UUID, who auth.Identity, op string) (backend, lock string, e error) {
 if e=ctx.Err();e!=nil{return}
 if tx==nil || tid==uuid.Nil || projectID==uuid.Nil || who.UserID==uuid.Nil || who.SessionID==uuid.Nil || (op!="mask.prompt" && op!="image.generate") {
  e=domain.NewError("VALIDATION_FAILED",422);return
 }
 e=tx.QueryRow(ctx,`SELECT backend,lock_id FROM photo.select_interactive_runtime($1,$2,$3,$4,$5)`,tid,projectID,who.UserID,who.SessionID,op).Scan(&backend,&lock)
 if errors.Is(e,pgx.ErrNoRows){e=domain.NewError("INTERACTIVE_CAPABILITY_UNAVAILABLE",409)}
 if e==nil {if _,err:=domain.ParseDigest(lock);err!=nil || (backend!="cuda" && backend!="mps" && backend!="cpu"){e=domain.NewError("INTERACTIVE_CAPABILITY_UNAVAILABLE",409)}}
 if e==nil {e=ctx.Err()};return
}

func object(raw json.RawMessage, max int) (map[string]any, error) {
	if len(raw) < 2 || len(raw) > max {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	return v, nil
}
func array(raw json.RawMessage, max int) ([]any, error) {
	if len(raw) == 0 {
		return []any{}, nil
	}
	if len(raw) > max {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	var v []any
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	return v, nil
}
func refFromMask(v any) (ArtifactRef, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return ArtifactRef{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	a, ok := m["artifact"].(map[string]any)
	if !ok {
		return ArtifactRef{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	idraw, ok := a["artifact_id"].(string)
	if !ok {
		return ArtifactRef{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	id, e := uuid.Parse(idraw)
	sha, ok := a["sha256"].(string)
	if e != nil || !ok {
		return ArtifactRef{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if _, e = domain.ParseDigest(sha); e != nil {
		return ArtifactRef{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return ArtifactRef{id, sha}, nil
}
func insertBase(ctx context.Context, tx pgx.Tx, tid, user uuid.UUID, kind string, s source, cfg any) (uuid.UUID, error) {
	snapshot, run := uuid.New(), uuid.New()
	digest, e := canonical.Hash(map[string]any{"artifact_id": s.ArtifactID, "sha256": s.SHA})
	if e != nil {
		return uuid.Nil, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,artifact_id) VALUES($1,$2,'input','user',$3,$4,1,$5)`, snapshot, tid, user, digest, s.ArtifactID); e != nil {
		return uuid.Nil, e
	}
	raw, e := canonical.Marshal(cfg)
	if e != nil {
		return uuid.Nil, e
	}
	h, e := canonical.Hash(cfg)
	if e != nil {
		return uuid.Nil, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES($1,$2,$3,'queued',$4,$5,$6,$7,'fail_fast',false)`, run, tid, kind, snapshot, raw, h, user)
	return run, e
}
func insertTask(ctx context.Context, tx pgx.Tx, tid, run, task uuid.UUID, kind, op string, ordinal int, s source, inputs []ArtifactRef, params any, backend, lock string) error {
	raw, e := canonical.Marshal(params)
	if e != nil {
		return e
	}
	h, e := canonical.Hash(params)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,ordinal,operation,source_artifact_id,declared_inputs,params,params_sha256,backend,runtime_lock_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tid, run, task, kind, ordinal, op, s.ArtifactID, inputs, raw, h, backend, nullable(lock))
	return e
}
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func (s Service) CreateMask(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req MaskCreate) (idempotency.Response, error) {
	var out idempotency.Response
	if req.AssetVersionID == uuid.Nil || req.ObjectID != nil && *req.ObjectID == uuid.Nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if _, e := domain.ParseDigest(req.InputSHA256); e != nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		src, e := loadSource(ctx, tx, who, tid, req.AssetVersionID, role)
		if e != nil {
			return e
		}
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Mask.Create", Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			// Idempotency conflict/replay is decided first. A new invalid binding
			// rolls back this transaction, including its tentative idempotency row.
			if req.InputSHA256 != src.SHA {
				return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
			}
			shapes, points, manual, e := parseMaskInputs(req)
			if e != nil {
				return idempotency.Response{}, e
			}
			task := uuid.New()
			cfg := map[string]any{"schema_version": 1, "asset_version_id": req.AssetVersionID, "source_sha256": req.InputSHA256, "operation": map[bool]string{true: "mask.rasterize", false: "mask.prompt"}[manual]}
			run, e := insertBase(ctx, tx, tid, who.UserID, "mask", src, cfg)
			if e != nil {
				return idempotency.Response{}, e
			}
			if manual {
				template := map[string]any{"source_sha256": req.InputSHA256, "object_id": req.ObjectID, "shapes": shapes}
				e = insertTask(ctx, tx, tid, run, task, "mask_manual", "mask.rasterize", 0, src, []ArtifactRef{{src.ArtifactID, src.SHA}}, template, "cpu", "")
			} else {
				backend, lock, e := runtime(ctx, tx, tid, src.ProjectID, who, "mask.prompt")
				if e != nil {
					return idempotency.Response{}, e
				}
				selector, box, e := parsePromptOptions(req)
				if e != nil {
					return idempotency.Response{}, e
				}
				params := map[string]any{"image": ArtifactRef{src.ArtifactID, src.SHA}, "source_sha256": req.InputSHA256, "object_id": req.ObjectID, "selector": selector, "points": points, "model_id": "mobilesam", "model_lock_id": lock, "preprocessing_id": "mobilesam_long1024_v1"}
				if box != nil {
					params["box"] = box
				}
				e = insertTask(ctx, tx, tid, run, task, "mask_prompt", "mask.prompt", 0, src, []ArtifactRef{{src.ArtifactID, src.SHA}}, params, backend, lock)
			}
			if e != nil {
				return idempotency.Response{}, e
			}
			body, e := canonical.Marshal(MaskAccepted{run, req.InputSHA256, "queued"})
			return idempotency.Response{Status: 202, Body: body, ResourceID: &run}, e
		})
		if e != nil {
			return e
		}
		// Receipt acquisition may wait behind another request. Both new work
		// and accepted replay must still belong to a currently visible source.
		if e = validateReceipt(out, req.InputSHA256, key, false); e != nil {
			return e
		}
		_, e = loadSource(ctx, tx, who, tid, req.AssetVersionID, role)
		return e
	})
	return out, err
}
func (s Service) Generate(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req GenerateRequest) (idempotency.Response, error) {
 return s.generate(ctx,who,tid,key,req,false)
}
// ReplayGeneration may read an existing receipt but never admits new GPU work.
func (s Service) ReplayGeneration(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req GenerateRequest) (idempotency.Response, error) {
 return s.generate(ctx,who,tid,key,req,true)
}
// AbandonGeneration serializes with normal admission under the SAME receipt
// scope/body/key. A prior accepted run wins; otherwise a durable tombstone wins.
// Generic validation/transport errors never establish that admission was absent.
func (s Service) AbandonGeneration(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req GenerateRequest) (idempotency.Response, error) {
	var out idempotency.Response
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		if _, e := loadSource(ctx, tx, who, tid, req.AssetVersionID, role); e != nil {
			return e
		}
		var e error
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Generation.CreateCandidate", Key: key}, req, time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), func() (idempotency.Response, error) {
			currentRole, e := tenant.AuthorizeInTransaction(ctx, tx, who, tid, domain.Viewer)
			if e != nil { return idempotency.Response{}, e }
			if _, e = loadSource(ctx, tx, who, tid, req.AssetVersionID, currentRole); e != nil { return idempotency.Response{}, e }
			body, e := canonical.Marshal(map[string]string{"status": "abandoned", "key": key})
			return idempotency.Response{Status: 200, Body: body}, e
		})
		if e != nil { return e }
		if e = validateReceipt(out, "", key, true); e != nil { return e }
		currentRole, e := tenant.AuthorizeInTransaction(ctx, tx, who, tid, domain.Viewer)
		if e != nil { return e }
		_, e = loadSource(ctx, tx, who, tid, req.AssetVersionID, currentRole)
		return e
	})
	if err != nil { return idempotency.Response{}, err }
	return out, nil
}

func (s Service) generate(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req GenerateRequest, replayOnly bool) (idempotency.Response, error) {
	var out idempotency.Response
	if req.AssetVersionID == uuid.Nil || req.DraftRevision < 0 || req.VariantCount < 1 || req.VariantCount > 4 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	mask, err := object(req.MaskRef, 256<<10)
	if err != nil {
		return out, err
	}
	protected, err := array(req.ProtectedRefs, 512<<10)
	if err != nil || len(protected) > 32 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	base, err := object(req.Params, 256<<10)
	if err != nil {
		return out, err
	}
	for _, k := range []string{"image", "mask", "protected", "confirmed", "model_id", "model_lock_id"} {
		if _, ok := base[k]; ok {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	seed, ok := base["seed"].(float64)
	if !ok || seed < 0 || seed > math.MaxUint32-float64(req.VariantCount-1) || seed != math.Trunc(seed) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		src, e := loadSource(ctx, tx, who, tid, req.AssetVersionID, role)
		if e != nil {
			return e
		}

		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Generation.CreateCandidate", Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			// Use may wait on another request. Recheck before admitting fresh work.
			currentRole, authErr := tenant.AuthorizeInTransaction(ctx, tx, who, tid, domain.Viewer)
			if authErr != nil {
				return idempotency.Response{}, authErr
			}
			src, authErr = loadSource(ctx, tx, who, tid, req.AssetVersionID, currentRole)
			if authErr != nil {
				return idempotency.Response{}, authErr
			}
			if replayOnly {
				return idempotency.Response{}, domain.NewError("GENERATION_REPLAY_PENDING", 409)
			}
			// Mutable draft/backend admission applies only to a new operation.
			var rev int64
			e = tx.QueryRow(ctx, `SELECT revision FROM photo.editor_drafts WHERE tenant_id=$1 AND asset_version_id=$2 AND user_id=$3`, tid, req.AssetVersionID, who.UserID).Scan(&rev)
			if errors.Is(e, pgx.ErrNoRows) && req.DraftRevision == 0 {
				e = nil
			}
			if e != nil {
				return idempotency.Response{}, e
			}
			if rev != req.DraftRevision {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			backend, lock, e := runtime(ctx, tx, tid, src.ProjectID, who, "image.generate")
			if e != nil {
				return idempotency.Response{}, e
			}
			run, e := insertBase(ctx, tx, tid, who.UserID, "preview", src, map[string]any{"schema_version": 1, "explicit_generation": true, "asset_version_id": req.AssetVersionID, "draft_revision": req.DraftRevision, "variant_count": req.VariantCount})
			if e != nil {
				return idempotency.Response{}, e
			}
			refs := []ArtifactRef{{src.ArtifactID, src.SHA}}
			r, e := refFromMask(mask)
			if e != nil {
				return idempotency.Response{}, e
			}
			refs = append(refs, r)
			for _, v := range protected {
				x, e := refFromMask(v)
				if e != nil {
					return idempotency.Response{}, e
				}
				refs = append(refs, x)
			}
			for i := 0; i < req.VariantCount; i++ {
				p := map[string]any{}
				for k, v := range base {
					p[k] = v
				}
				p["image"] = ArtifactRef{src.ArtifactID, src.SHA}
				p["mask"] = mask
				p["protected"] = protected
				p["confirmed"] = true
				p["model_id"] = "sd15_inpaint"
				p["model_lock_id"] = lock
				p["seed"] = uint32(seed) + uint32(i)
				if e = insertTask(ctx, tx, tid, run, uuid.New(), "generation", "image.generate", i, src, refs, p, backend, lock); e != nil {
					return idempotency.Response{}, e
				}
			}
			body, e := canonical.Marshal(RunAccepted{run, "queued"})
			return idempotency.Response{Status: 202, Body: body, ResourceID: &run}, e
		})
		if e != nil {
			return e
		}
		if out.Status == 200 {
			// Only AbandonGeneration writes a 200 tombstone into this scope.
			if e = validateReceipt(out, "", key, true); e != nil { return e }
			return domain.NewError("GENERATION_REQUEST_ABANDONED", 409)
		}
		if e = validateReceipt(out, "", key, false); e != nil { return e }
		// A committed receipt bypasses the callback; authorize again after Use's
		// receipt lock wait before exposing its saved body to this caller.
		currentRole, e := tenant.AuthorizeInTransaction(ctx, tx, who, tid, domain.Viewer)
		if e != nil {
			return e
		}
		_, e = loadSource(ctx, tx, who, tid, req.AssetVersionID, currentRole)
		return e
	})
	if err != nil {
		return idempotency.Response{}, err
	}
	return out, nil
}

func (s Service) Preview(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req PreviewRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if req.AssetVersionID == uuid.Nil || req.DraftRevision < 0 || req.Generation ||
		(req.Scale != "proxy" && req.Scale != "full") || req.StyleVersionID != nil && *req.StyleVersionID == uuid.Nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	modules, err := object(req.Params, 512<<10)
	if err != nil || len(modules) == 0 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if schema, wrapped := modules["schema_version"]; wrapped {
		if schema != float64(1) {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		value, ok := modules["modules"].(map[string]any)
		if !ok || len(value) == 0 {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		modules = value
	}
	allowed := map[string]bool{"strength": true, "global": true, "local": true, "beauty": true, "defects": true, "geometry": true, "extensions": true}
	for name := range modules {
		if !allowed[name] {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	if req.ROI != nil && ((*req.ROI)[0] < 0 || (*req.ROI)[1] < 0 || (*req.ROI)[2] < 1 || (*req.ROI)[3] < 1) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		src, e := loadSource(ctx, tx, who, tid, req.AssetVersionID, role)
		if e != nil {
			return e
		}

		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Preview.Request", Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			var revision int64
			e = tx.QueryRow(ctx, `SELECT revision FROM photo.editor_drafts WHERE tenant_id=$1 AND asset_version_id=$2 AND user_id=$3`, tid, req.AssetVersionID, who.UserID).Scan(&revision)
			if errors.Is(e, pgx.ErrNoRows) && req.DraftRevision == 0 {
				e = nil
			}
			if e != nil {
				return idempotency.Response{}, e
			}
			if revision != req.DraftRevision {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			var styleRef *ArtifactRef
			if req.StyleVersionID != nil {
				var ref ArtifactRef
				var approval, mode string
				e = tx.QueryRow(ctx, `SELECT v.export_artifact_id,v.config_hash,v.approval_status,v.config_json->>'mode' FROM photo.style_versions v JOIN photo.styles s ON s.tenant_id=v.tenant_id AND s.id=v.style_id JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.export_artifact_id AND a.sha256=v.config_hash AND a.state='ready' AND a.object_version IS NOT NULL WHERE v.tenant_id=$1 AND v.id=$2 AND s.project_id=$3 AND s.archived_at IS NULL`, tid, *req.StyleVersionID, src.ProjectID).Scan(&ref.ArtifactID, &ref.SHA256, &approval, &mode)
				if errors.Is(e, pgx.ErrNoRows) {
					return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
				}
				if e != nil {
					return idempotency.Response{}, e
				}
				if approval != "accepted" || mode != "fixed" {
					return idempotency.Response{}, domain.NewError("STYLE_NOT_ACCEPTED", 409)
				}
				styleRef = &ref
			}

			// Completed idempotent responses remain recoverable after a worker
			// disappears. Only a new preview must pass current CPU admission.
			if e := admitPreviewWorker(ctx, tx); e != nil {
				return idempotency.Response{}, e
			}
			run, e := insertBase(ctx, tx, tid, who.UserID, "preview", src, map[string]any{"schema_version": 1, "explicit_generation": false, "asset_version_id": req.AssetVersionID, "draft_revision": req.DraftRevision, "style_version_id": req.StyleVersionID, "scale": req.Scale, "roi": req.ROI})
			if e != nil {
				return idempotency.Response{}, e
			}
			refs := []ArtifactRef{{src.ArtifactID, src.SHA}}
			if styleRef != nil {
				refs = append(refs, *styleRef)
			}
			params := map[string]any{"image": ArtifactRef{src.ArtifactID, src.SHA}, "style": styleRef, "modules": modules, "scale": req.Scale, "roi": req.ROI}
			if e = insertTask(ctx, tx, tid, run, uuid.New(), "preview", "editor.preview", 0, src, refs, params, "cpu", ""); e != nil {
				return idempotency.Response{}, e
			}
			if req.DraftRevision > 0 {
				tag, x := tx.Exec(ctx, `UPDATE photo.editor_drafts SET last_preview_run_id=$4 WHERE tenant_id=$1 AND asset_version_id=$2 AND user_id=$3 AND revision=$5`, tid, req.AssetVersionID, who.UserID, run, req.DraftRevision)
				if x != nil {
					return idempotency.Response{}, x
				}
				if tag.RowsAffected() != 1 {
					return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
				}
			}
			body, e := canonical.Marshal(RunAccepted{run, "queued"})
			return idempotency.Response{Status: 202, Body: body, ResourceID: &run}, e
		})
		if e != nil {
			return e
		}
		if e = validateReceipt(out, "", key, false); e != nil { return e }
		_, e = loadSource(ctx, tx, who, tid, req.AssetVersionID, role)
		return e
	})
	return out, err
}
