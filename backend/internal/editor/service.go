package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
)

type Service struct{ DB *pgxpool.Pool }

type Draft struct {
	ID               *uuid.UUID      `json:"id,omitempty"`
	AssetVersionID   uuid.UUID       `json:"asset_version_id"`
	Revision         string          `json:"revision"`
	Params           json.RawMessage `json:"params"`
	MaskRefs         json.RawMessage `json:"mask_refs"`
	LastPreviewRunID *uuid.UUID      `json:"last_preview_run_id,omitempty"`
	SourceSHA256     string          `json:"source_sha256"`
}
type DraftSave struct {
	ExpectedRevision int64           `json:"expected_revision"`
	AssetVersionID   uuid.UUID       `json:"asset_version_id"`
	Params           json.RawMessage `json:"params"`
	MaskRefs         json.RawMessage `json:"mask_refs"`
}
type OverrideCreate struct {
	AssetVersionID uuid.UUID       `json:"asset_version_id"`
	InputSHA256    string          `json:"input_sha256"`
	BaseRevision   *int64          `json:"base_revision,omitempty"`
	Params         json.RawMessage `json:"params"`
}
type Override struct {
	ID             uuid.UUID       `json:"id"`
	AssetVersionID uuid.UUID       `json:"asset_version_id"`
	InputSHA256    string          `json:"input_sha256"`
	Revision       string          `json:"revision"`
	Config         json.RawMessage `json:"config"`
	ConfigHash     string          `json:"config_hash"`
	CreatedAt      time.Time       `json:"created_at"`
}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var maskBindingIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$`)

type overrideMaskBinding struct {
	BindingID string         `json:"binding_id"`
	Purpose   string         `json:"purpose"`
	Mask      review.MaskRef `json:"mask"`
	Confirmed bool           `json:"confirmed"`
	Strength  float64        `json:"strength"`
}

type overrideEnvelope struct {
	SchemaVersion        int                        `json:"schema_version"`
	InputSHA256          string                     `json:"input_sha256"`
	ObjectID             *string                    `json:"object_id,omitempty"`
	Modules              map[string]json.RawMessage `json:"modules"`
	AcceptedCandidateIDs []string                   `json:"accepted_candidate_ids,omitempty"`
	RejectedCandidateIDs []string                   `json:"rejected_candidate_ids,omitempty"`
	MaskRefs             []json.RawMessage          `json:"mask_refs,omitempty"`
	ProtectedPolygons    []json.RawMessage          `json:"protected_polygons,omitempty"`
	ManualLandmarks      json.RawMessage            `json:"manual_landmarks,omitempty"`
	Revision             int64                      `json:"revision"`
}

func validateOverride(raw json.RawMessage, inputSHA string) error {
	var value overrideEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || value.SchemaVersion != 1 || value.InputSHA256 != inputSHA ||
		!sha256Pattern.MatchString(value.InputSHA256) || value.Revision < 1 || len(value.Modules) == 0 ||
		len(value.AcceptedCandidateIDs) > 128 || len(value.RejectedCandidateIDs) > 128 ||
		len(value.MaskRefs) > 128 || len(value.ProtectedPolygons) > 128 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if review.ValidateOverrideManualGeometry(value.ProtectedPolygons, value.ManualLandmarks, inputSHA) != nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	allowed := map[string]bool{"strength": true, "global": true, "local": true, "beauty": true, "defects": true, "geometry": true, "extensions": true}
	for name, body := range value.Modules {
		if !allowed[name] || len(body) == 0 || string(body) == "null" {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	decisions := map[string]bool{}
	for _, id := range value.AcceptedCandidateIDs {
		if id == "" || decisions[id] {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		decisions[id] = true
	}
	for _, id := range value.RejectedCandidateIDs {
		if id == "" || decisions[id] {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		decisions[id] = true
	}
	bindings := map[string]bool{}
	for _, rawBinding := range value.MaskRefs {
		var binding overrideMaskBinding
		decoder := json.NewDecoder(bytes.NewReader(rawBinding))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&binding) != nil || !maskBindingIDPattern.MatchString(binding.BindingID) || bindings[binding.BindingID] || binding.Strength < 0 || binding.Strength > 1 || binding.Mask.Artifact.ID == uuid.Nil || !sha256Pattern.MatchString(binding.Mask.Artifact.SHA256) || binding.Mask.Confidence < 0 || binding.Mask.Confidence > 1 {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		bindings[binding.BindingID] = true
		selectors := map[string]map[string]bool{
			"portrait_teeth": {"teeth": true, "manual": true}, "portrait_sclera": {"sclera": true, "manual": true},
			"portrait_shine": {"skin": true, "manual": true}, "portrait_under_eye": {"skin": true, "manual": true},
			"portrait_cloth": {"clothing": true, "manual": true}, "portrait_glasses": {"glasses": true, "manual": true},
			"inpaint_target": {"manual": true}, "generation_target": {"manual": true},
		}
		if binding.Purpose == "protected" {
			if !binding.Mask.Protected {
				return domain.NewError("VALIDATION_FAILED", 422)
			}
		} else if allowedSelectors, ok := selectors[binding.Purpose]; !ok || binding.Mask.Protected || !allowedSelectors[binding.Mask.Selector] {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		if (binding.Purpose == "portrait_cloth" || binding.Purpose == "portrait_glasses" || binding.Purpose == "inpaint_target" || binding.Purpose == "generation_target") && !binding.Confirmed {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	return nil
}

// ValidateOverride allows V2 immutable document admission to reuse the same
// source-bound override contract as the existing editor API.
func ValidateOverride(raw json.RawMessage, inputSHA string) error {
	return validateOverride(raw, inputSHA)
}

func overrideMaskBindings(raw json.RawMessage) ([]overrideMaskBinding, error) {
	var envelope overrideEnvelope
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	bindings := make([]overrideMaskBinding, 0, len(envelope.MaskRefs))
	for _, entry := range envelope.MaskRefs {
		var binding overrideMaskBinding
		if json.Unmarshal(entry, &binding) != nil {
			return nil, domain.NewError("VALIDATION_FAILED", 422)
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

func validateOverrideMaskProvenance(ctx context.Context, tx pgx.Tx, tenantID, versionID uuid.UUID, bindings []overrideMaskBinding) error {
	for _, binding := range bindings {
		var valid bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS(
			 SELECT FROM photo.artifacts a
			 JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id AND t.state='succeeded'
			 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
			 JOIN photo.interactive_tasks i ON i.tenant_id=t.tenant_id AND i.task_id=t.id
			 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=$3 AND v.master_artifact_id=i.source_artifact_id
			 WHERE a.tenant_id=$1 AND a.id=$2 AND a.sha256=$4 AND a.state='ready' AND a.object_version IS NOT NULL
			  AND ((t.operation='mask.rasterize' AND a.output_role=CASE WHEN $5 THEN 'mask_protected' ELSE 'mask_target' END)
			       OR (t.operation='mask.prompt' AND NOT $5 AND a.output_role='prompt_mask'))
			  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
			)`, tenantID, binding.Mask.Artifact.ID, versionID, binding.Mask.Artifact.SHA256, binding.Mask.Protected).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return domain.NewError("OVERRIDE_MASK_INVALID", 409)
		}
	}
	return nil
}

func validObject(raw json.RawMessage, max int, requireSchema bool) error {
	if len(raw) == 0 || len(raw) > max {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if requireSchema {
		v, ok := value["schema_version"].(float64)
		if !ok || v != 1 {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	return nil
}
func validJSON(raw json.RawMessage, max int) error {
	if len(raw) == 0 || len(raw) > max {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	var value any
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return nil
}
func authorizeAsset(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, assetID uuid.UUID, role, required domain.Role, lock bool) (uuid.UUID, string, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF a"
	}
	var versionID uuid.UUID
	var projectID uuid.UUID
	var hash string
	err := tx.QueryRow(ctx, `SELECT a.project_id,v.id,v.content_hash FROM photo.assets a JOIN LATERAL (SELECT id,content_hash FROM photo.asset_versions WHERE tenant_id=a.tenant_id AND asset_id=a.id ORDER BY version_no DESC LIMIT 1) v ON true WHERE a.tenant_id=$1 AND a.id=$2 AND a.deleted_at IS NULL`+suffix, tid, assetID).Scan(&projectID, &versionID, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return uuid.Nil, "", err
	}
	_, err = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, required, false)
	return versionID, hash, err
}
func authorizeVersion(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, versionID uuid.UUID, role, required domain.Role, lock bool) (string, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF v"
	}
	var projectID uuid.UUID
	var hash string
	err := tx.QueryRow(ctx, `SELECT a.project_id,v.content_hash FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE v.tenant_id=$1 AND v.id=$2 AND a.deleted_at IS NULL`+suffix, tid, versionID).Scan(&projectID, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return "", err
	}
	_, err = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, required, false)
	return hash, err
}
func (s Service) GetDraft(ctx context.Context, who auth.Identity, tid, assetID uuid.UUID) (Draft, error) {
	out := Draft{Revision: "0", Params: json.RawMessage(`{}`), MaskRefs: json.RawMessage(`[]`)}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		versionID, hash, e := authorizeAsset(ctx, tx, who, tid, assetID, role, domain.Viewer, false)
		if e != nil {
			return e
		}
		out.AssetVersionID = versionID
		out.SourceSHA256 = hash
		var id uuid.UUID
		var rev int64
		e = tx.QueryRow(ctx, `SELECT id,revision,params,mask_refs,last_preview_run_id FROM photo.editor_drafts WHERE tenant_id=$1 AND asset_version_id=$2 AND user_id=$3`, tid, versionID, who.UserID).Scan(&id, &rev, &out.Params, &out.MaskRefs, &out.LastPreviewRunID)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		out.ID, out.Revision = &id, strconv.FormatInt(rev, 10)
		return nil
	})
	return out, err
}
func (s Service) SaveDraft(ctx context.Context, who auth.Identity, tid, assetID uuid.UUID, expected int64, req DraftSave) (Draft, error) {
	var out Draft
	if expected < 0 || req.ExpectedRevision != expected || req.AssetVersionID == uuid.Nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := validObject(req.Params, 256<<10, false); err != nil {
		return out, err
	}
	if err := validJSON(req.MaskRefs, 256<<10); err != nil {
		return out, err
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		latest, hash, e := authorizeAsset(ctx, tx, who, tid, assetID, role, domain.Editor, true)
		if e != nil {
			return e
		}
		if latest != req.AssetVersionID {
			return domain.NewError("SOURCE_VERSION_MISMATCH", 409)
		}
		var current int64
		e = tx.QueryRow(ctx, `SELECT revision FROM photo.editor_drafts WHERE tenant_id=$1 AND asset_version_id=$2 AND user_id=$3 FOR UPDATE`, tid, latest, who.UserID).Scan(&current)
		if errors.Is(e, pgx.ErrNoRows) {
			if expected != 0 {
				return domain.NewError("REVISION_CONFLICT", 409)
			}
			var id uuid.UUID
			var rev int64
			e = tx.QueryRow(ctx, `INSERT INTO photo.editor_drafts(tenant_id,asset_version_id,user_id,params,mask_refs) VALUES($1,$2,$3,$4,$5) RETURNING id,revision`, tid, latest, who.UserID, req.Params, req.MaskRefs).Scan(&id, &rev)
			out = Draft{ID: &id, AssetVersionID: latest, Revision: strconv.FormatInt(rev, 10), Params: req.Params, MaskRefs: req.MaskRefs, SourceSHA256: hash}
			return e
		}
		if e != nil {
			return e
		}
		if current != expected {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		var id uuid.UUID
		var rev int64
		e = tx.QueryRow(ctx, `UPDATE photo.editor_drafts SET params=$4,mask_refs=$5,revision=revision+1 WHERE tenant_id=$1 AND asset_version_id=$2 AND user_id=$3 RETURNING id,revision,last_preview_run_id`, tid, latest, who.UserID, req.Params, req.MaskRefs).Scan(&id, &rev, &out.LastPreviewRunID)
		out = Draft{ID: &id, AssetVersionID: latest, Revision: strconv.FormatInt(rev, 10), Params: req.Params, MaskRefs: req.MaskRefs, LastPreviewRunID: out.LastPreviewRunID, SourceSHA256: hash}
		return e
	})
	return out, err
}
func (s Service) CreateOverride(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req OverrideCreate) (idempotency.Response, error) {
	var out idempotency.Response
	if req.AssetVersionID == uuid.Nil || !sha256Pattern.MatchString(req.InputSHA256) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := validObject(req.Params, 512<<10, true); err != nil {
		return out, err
	}
	if err := validateOverride(req.Params, req.InputSHA256); err != nil {
		return out, err
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var e error
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Override.Create", Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			hash, e := authorizeVersion(ctx, tx, who, tid, req.AssetVersionID, role, domain.Editor, true)
			if e != nil {
				return idempotency.Response{}, e
			}
			if hash != req.InputSHA256 {
				return idempotency.Response{}, domain.NewError("OVERRIDE_SOURCE_MISMATCH", 409)
			}
			bindings, bindingErr := overrideMaskBindings(req.Params)
			if bindingErr != nil {
				return idempotency.Response{}, bindingErr
			}
			if bindingErr = validateOverrideMaskProvenance(ctx, tx, tid, req.AssetVersionID, bindings); bindingErr != nil {
				return idempotency.Response{}, bindingErr
			}
			var last int64
			if e = tx.QueryRow(ctx, `SELECT coalesce(max(revision),0) FROM photo.overrides WHERE tenant_id=$1 AND asset_version_id=$2`, tid, req.AssetVersionID).Scan(&last); e != nil {
				return idempotency.Response{}, e
			}
			if req.BaseRevision != nil && *req.BaseRevision != last {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			var normalized map[string]any
			if json.Unmarshal(req.Params, &normalized) != nil {
				return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
			}
			normalized["revision"] = last + 1
			req.Params, e = canonical.Marshal(normalized)
			if e != nil {
				return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
			}
			configHash, e := canonical.Hash(json.RawMessage(req.Params))
			if e != nil {
				return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
			}
			item := Override{AssetVersionID: req.AssetVersionID, InputSHA256: req.InputSHA256, Revision: strconv.FormatInt(last+1, 10), Config: req.Params, ConfigHash: configHash}
			e = tx.QueryRow(ctx, `INSERT INTO photo.overrides(tenant_id,asset_version_id,input_sha256,revision,config,config_hash,created_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,created_at`, tid, req.AssetVersionID, req.InputSHA256, last+1, req.Params, configHash, who.UserID).Scan(&item.ID, &item.CreatedAt)
			if e != nil {
				return idempotency.Response{}, e
			}
			body, e := canonical.Marshal(item)
			return idempotency.Response{Status: 201, Body: body, ResourceID: &item.ID}, e
		})
		return e
	})
	return out, err
}
