package collection

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/tenant"
)

type SharePermissions struct {
	View     bool `json:"view"`
	Download bool `json:"download"`
	Upload   bool `json:"upload"`
}
type ShareLimits struct {
	MaxUploadFiles      int64    `json:"max_upload_files"`
	MaxUploadBytesTotal int64    `json:"max_upload_bytes_total"`
	MaxFileBytes        int64    `json:"max_file_bytes"`
	AllowedFormats      []string `json:"allowed_formats"`
}
type OptionalTime struct {
	Set   bool
	Value *time.Time
}

func (v *OptionalTime) UnmarshalJSON(raw []byte) error {
	v.Set = true
	if string(raw) == "null" {
		v.Value = nil
		return nil
	}
	var value time.Time
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	value = value.UTC()
	v.Value = &value
	return nil
}

type ShareCreate struct {
	Label          string           `json:"label"`
	ExpiresAt      OptionalTime     `json:"expires_at,omitempty"`
	Permissions    SharePermissions `json:"permissions"`
	Limits         ShareLimits      `json:"limits"`
	Moderation     string           `json:"moderation"`
	AllowedPresets []uuid.UUID      `json:"allowed_presets"`
}
type SharePatch struct {
	Label          *string           `json:"label,omitempty"`
	ExpiresAt      OptionalTime      `json:"expires_at,omitempty"`
	Permissions    *SharePermissions `json:"permissions,omitempty"`
	Limits         *ShareLimits      `json:"limits,omitempty"`
	Moderation     *string           `json:"moderation,omitempty"`
	AllowedPresets *[]uuid.UUID      `json:"allowed_presets,omitempty"`
}
type Share struct {
	ID             uuid.UUID       `json:"id"`
	CollectionID   uuid.UUID       `json:"collection_id"`
	Label          string          `json:"label"`
	Permissions    json.RawMessage `json:"permissions"`
	PolicyVersion  string          `json:"policy_version"`
	ExpiresAt      *time.Time      `json:"expires_at,omitempty"`
	RevokedAt      *time.Time      `json:"revoked_at,omitempty"`
	Limits         json.RawMessage `json:"limits"`
	Moderation     string          `json:"moderation"`
	AllowedPresets json.RawMessage `json:"allowed_presets"`
	CreatedAt      time.Time       `json:"created_at"`
}
type ShareCreated struct {
	Share
	Secret   string `json:"secret,omitempty"`
	Replayed bool   `json:"-"`
}

type preparedShare struct {
	Label       string
	ExpiresAt   *time.Time
	Permissions SharePermissions
	Limits      ShareLimits
	Moderation  string
	Presets     []uuid.UUID
}

const shareFields = `l.id,l.collection_id,l.label,l.permissions,l.policy_version,l.expires_at,l.revoked_at,l.limits,l.moderation,l.allowed_presets,l.created_at`

func scanShare(row scanner) (Share, error) {
	var out Share
	var revision int64
	err := row.Scan(&out.ID, &out.CollectionID, &out.Label, &out.Permissions, &revision, &out.ExpiresAt, &out.RevokedAt, &out.Limits, &out.Moderation, &out.AllowedPresets, &out.CreatedAt)
	out.PolicyVersion = strconv.FormatInt(revision, 10)
	return out, err
}

var shareFormats = map[string]bool{"jpeg": true, "jpg": true, "png": true, "tiff": true, "tif": true, "dng": true, "cr2": true, "nef": true, "arw": true, "raf": true, "orf": true, "rw2": true}

func normalizeShare(label string, permissions SharePermissions, limits ShareLimits, moderation string, presets []uuid.UUID, expires *time.Time, now time.Time) (string, ShareLimits, string, []uuid.UUID, error) {
	label = strings.TrimSpace(label)
	moderation = strings.TrimSpace(moderation)
	if len([]rune(label)) > 120 || (!permissions.View && !permissions.Download && !permissions.Upload) || moderation != "owner_review" && moderation != "auto_after_validation" || limits.MaxUploadFiles < 0 || limits.MaxUploadFiles > 10000 || limits.MaxUploadBytesTotal < 0 || limits.MaxUploadBytesTotal > 1099511627776 || limits.MaxFileBytes < 1 || limits.MaxFileBytes > 536870912 || limits.MaxFileBytes > limits.MaxUploadBytesTotal || len(limits.AllowedFormats) > 16 || len(presets) > 100 {
		return "", limits, "", nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if expires != nil && (!expires.After(now) || expires.After(now.Add(366*24*time.Hour))) {
		return "", limits, "", nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	seen := map[string]bool{}
	for i, f := range limits.AllowedFormats {
		f = strings.ToLower(strings.TrimSpace(f))
		if !shareFormats[f] || seen[f] {
			return "", limits, "", nil, domain.NewError("VALIDATION_FAILED", 422)
		}
		seen[f] = true
		limits.AllowedFormats[i] = f
	}
	sort.Strings(limits.AllowedFormats)
	sort.Slice(presets, func(i, j int) bool { return presets[i].String() < presets[j].String() })
	for i, id := range presets {
		if id == uuid.Nil || (i > 0 && id == presets[i-1]) {
			return "", limits, "", nil, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	if permissions.Download && !permissions.View {
		return "", limits, "", nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	return label, limits, moderation, presets, nil
}
func (s Service) shareSecret(id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString(auth.MAC(s.Pepper, "share-bearer", id.String()))
}
func prepareShare(req ShareCreate, now time.Time) (preparedShare, error) {
	expires := req.ExpiresAt.Value
	if !req.ExpiresAt.Set {
		value := now.Add(30 * 24 * time.Hour)
		expires = &value
	}
	label, limits, moderation, presets, err := normalizeShare(req.Label, req.Permissions, req.Limits, req.Moderation, append([]uuid.UUID(nil), req.AllowedPresets...), expires, now)
	return preparedShare{Label: label, ExpiresAt: expires, Permissions: req.Permissions, Limits: limits, Moderation: moderation, Presets: presets}, err
}

func (s Service) insertShare(ctx context.Context, tx pgx.Tx, tid, collectionID, actor uuid.UUID, prepared preparedShare) (Share, error) {
	if err := verifyPresets(ctx, tx, tid, prepared.Presets); err != nil {
		return Share{}, err
	}
	id := uuid.New()
	secret := s.shareSecret(id)
	permissionsJSON, err := canonical.Marshal(prepared.Permissions)
	if err != nil {
		return Share{}, err
	}
	limitsJSON, err := canonical.Marshal(prepared.Limits)
	if err != nil {
		return Share{}, err
	}
	presetsJSON, err := canonical.Marshal(prepared.Presets)
	if err != nil {
		return Share{}, err
	}
	return scanShare(tx.QueryRow(ctx, `INSERT INTO photo.share_links AS l(id,tenant_id,collection_id,secret_hash,label,permissions,expires_at,limits,moderation,allowed_presets,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+shareFields,
		id, tid, collectionID, auth.MAC(s.Pepper, "share-secret", id.String(), secret), prepared.Label, permissionsJSON, prepared.ExpiresAt, limitsJSON, prepared.Moderation, presetsJSON, actor))
}
func verifyPresets(ctx context.Context, tx pgx.Tx, tid uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM photo.export_presets WHERE tenant_id=$1 AND id=ANY($2::uuid[]) AND archived_at IS NULL`, tid, ids).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return domain.NewError("NOT_FOUND", 404)
	}
	return nil
}

func (s Service) CreateShare(ctx context.Context, who auth.Identity, tid, collectionID uuid.UUID, key string, req ShareCreate) (ShareCreated, error) {
	var result ShareCreated
	now := time.Now().UTC()
	prepared, err := prepareShare(req, now)
	if err != nil {
		return result, err
	}
	if len(s.Pepper) < 32 {
		return result, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if err := currentEditorProject(ctx, tx, who, tid, collectionID, role); err != nil {
			return err
		}
		var response idempotency.Response
		request := struct {
			Collection  uuid.UUID        `json:"collection_id"`
			Label       string           `json:"label"`
			Expires     *time.Time       `json:"expires_at"`
			Permissions SharePermissions `json:"permissions"`
			Limits      ShareLimits      `json:"limits"`
			Moderation  string           `json:"moderation"`
			Presets     []uuid.UUID      `json:"allowed_presets"`
		}{collectionID, prepared.Label, prepared.ExpiresAt, prepared.Permissions, prepared.Limits, prepared.Moderation, prepared.Presets}
		response, err = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Share.Create", Key: key}, request, now.Add(24*time.Hour), func() (idempotency.Response, error) {
			collection, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
			if err != nil {
				return idempotency.Response{}, err
			}
			if collection.State == "archived" {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			share, err := s.insertShare(ctx, tx, tid, collectionID, who.UserID, prepared)
			if err != nil {
				return idempotency.Response{}, err
			}
			if _, err = tx.Exec(ctx, `UPDATE photo.collections SET state='published',revision=revision+1 WHERE tenant_id=$1 AND id=$2 AND state='draft'`, tid, collectionID); err != nil {
				return idempotency.Response{}, err
			}
			if err = audit(ctx, tx, who, tid, collectionID, "share.create"); err != nil {
				return idempotency.Response{}, err
			}
			body, err := canonical.Marshal(ShareCreated{Share: share})
			return idempotency.Response{Status: 201, Body: body, ResourceID: &share.ID}, err
		})
		if err != nil {
			return err
		}
		if err = json.Unmarshal(response.Body, &result); err != nil {
			return err
		}
		result.Replayed = response.Replayed
		if !response.Replayed && response.ResourceID != nil {
			result.Secret = s.shareSecret(*response.ResourceID)
		}
		return nil
	})
	return result, err
}

func (s Service) ListShares(ctx context.Context, who auth.Identity, tid, collectionID uuid.UUID, limit int, raw string) (pagination.Page[Share], error) {
	out := pagination.Page[Share]{Items: []Share{}}
	if limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: collectionID.String(), Sort: "share:created_at,id"}
	var aid *uuid.UUID
	var at *time.Time
	if raw != "" {
		c, e := s.Cursors.Decode(raw, scope)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, e := uuid.Parse(c.LastID)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		p, e := time.Parse(time.RFC3339Nano, c.LastPosition)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		aid, at = &id, &p
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if err := currentEditorProject(ctx, tx, who, tid, collectionID, role); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+shareFields+` FROM photo.share_links l WHERE l.tenant_id=$1 AND l.collection_id=$2 AND ($3::uuid IS NULL OR (l.created_at,l.id)>($4::timestamptz,$3::uuid)) ORDER BY l.created_at,l.id LIMIT $5`, tid, collectionID, aid, at, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanShare(rows)
			if e != nil {
				return e
			}
			out.Items = append(out.Items, v)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		scope.LastID, scope.LastPosition = last.ID.String(), last.CreatedAt.Format(time.RFC3339Nano)
		c, e := s.Cursors.Encode(scope)
		if e != nil {
			return out, e
		}
		out.NextCursor = &c
	}
	return out, nil
}

func (s Service) UpdateShare(ctx context.Context, who auth.Identity, tid, collectionID, shareID uuid.UUID, expected int64, patch SharePatch) (Share, error) {
	var out Share
	if expected < 1 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	now := time.Now().UTC()
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		collection, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
		if err != nil {
			return err
		}
		if collection.State == "archived" {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		current, err := scanShare(tx.QueryRow(ctx, `SELECT `+shareFields+` FROM photo.share_links l WHERE l.tenant_id=$1 AND l.collection_id=$2 AND l.id=$3 FOR UPDATE`, tid, collectionID, shareID))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if current.PolicyVersion != strconv.FormatInt(expected, 10) || current.RevokedAt != nil {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		var permissions SharePermissions
		var limits ShareLimits
		var presets []uuid.UUID
		if json.Unmarshal(current.Permissions, &permissions) != nil || json.Unmarshal(current.Limits, &limits) != nil || json.Unmarshal(current.AllowedPresets, &presets) != nil {
			return domain.NewError("INTERNAL_ERROR", 500)
		}
		label, moderation, expires := current.Label, current.Moderation, current.ExpiresAt
		if patch.Label != nil {
			label = *patch.Label
		}
		if patch.Moderation != nil {
			moderation = *patch.Moderation
		}
		if patch.ExpiresAt.Set {
			expires = patch.ExpiresAt.Value
		}
		if patch.Permissions != nil {
			permissions = *patch.Permissions
		}
		if patch.Limits != nil {
			limits = *patch.Limits
		}
		if patch.AllowedPresets != nil {
			presets = append([]uuid.UUID(nil), (*patch.AllowedPresets)...)
		}
		label, limits, moderation, presets, err = normalizeShare(label, permissions, limits, moderation, presets, expires, now)
		if err != nil {
			return err
		}
		if err = verifyPresets(ctx, tx, tid, presets); err != nil {
			return err
		}
		permissionsJSON, _ := canonical.Marshal(permissions)
		limitsJSON, _ := canonical.Marshal(limits)
		presetsJSON, _ := canonical.Marshal(presets)
		out, err = scanShare(tx.QueryRow(ctx, `UPDATE photo.share_links AS l SET label=$4,expires_at=$5,permissions=$6,limits=$7,moderation=$8,allowed_presets=$9,policy_version=policy_version+1 WHERE tenant_id=$1 AND collection_id=$2 AND id=$3 RETURNING `+shareFields, tid, collectionID, shareID, label, expires, permissionsJSON, limitsJSON, moderation, presetsJSON))
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.share_sessions SET revoked_at=coalesce(revoked_at,clock_timestamp()) WHERE tenant_id=$1 AND share_id=$2 AND revoked_at IS NULL`, tid, shareID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.download_jobs SET cancel_requested=true WHERE tenant_id=$1 AND share_id=$2 AND status NOT IN ('ready','failed','cancelled','expired')`, tid, shareID); err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, collectionID, "share.update")
	})
	return out, err
}

func (s Service) RevokeShare(ctx context.Context, who auth.Identity, tid, collectionID, shareID uuid.UUID) (Share, error) {
	var out Share
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if _, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true); err != nil {
			return err
		}
		var err error
		out, err = scanShare(tx.QueryRow(ctx, `UPDATE photo.share_links AS l SET revoked_at=coalesce(revoked_at,clock_timestamp()),policy_version=CASE WHEN revoked_at IS NULL THEN policy_version+1 ELSE policy_version END WHERE tenant_id=$1 AND collection_id=$2 AND id=$3 RETURNING `+shareFields, tid, collectionID, shareID))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.share_sessions SET revoked_at=coalesce(revoked_at,clock_timestamp()) WHERE tenant_id=$1 AND share_id=$2`, tid, shareID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.download_jobs SET cancel_requested=true WHERE tenant_id=$1 AND share_id=$2 AND status NOT IN ('ready','failed','cancelled','expired')`, tid, shareID); err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, collectionID, "share.revoke")
	})
	return out, err
}
