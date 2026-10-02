package legacyimport

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type Service struct{ DB *pgxpool.Pool }

type ReconcileRequest struct {
	PlanHash       string          `json:"plan_hash"`
	SourceRelative string          `json:"source_relative"`
	SourceSHA256   string          `json:"source_sha256"`
	UploadID       uuid.UUID       `json:"upload_id"`
	Provenance     json.RawMessage `json:"provenance,omitempty"`
}

type Record struct {
	ID             uuid.UUID       `json:"id"`
	PlanHash       string          `json:"plan_hash"`
	SourceRelative string          `json:"source_relative"`
	SourceSHA256   string          `json:"source_sha256"`
	AssetVersionID uuid.UUID       `json:"asset_version_id"`
	UploadID       uuid.UUID       `json:"upload_id"`
	Provenance     json.RawMessage `json:"provenance"`
}

func normalize(raw ReconcileRequest) (ReconcileRequest, []byte, error) {
	raw.SourceRelative = strings.TrimSpace(raw.SourceRelative)
	if _, err := domain.ParseDigest(raw.PlanHash); err != nil {
		return raw, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if _, err := domain.ParseDigest(raw.SourceSHA256); err != nil {
		return raw, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	clean := path.Clean(raw.SourceRelative)
	if raw.UploadID == uuid.Nil || raw.SourceRelative == "" || len(raw.SourceRelative) > 1024 || clean != raw.SourceRelative || clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.ContainsAny(clean, "\\\x00") {
		return raw, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if len(raw.Provenance) == 0 {
		raw.Provenance = json.RawMessage(`{}`)
	}
	if len(raw.Provenance) > 65536 {
		return raw, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	provenance, err := canonical.Bytes(raw.Provenance)
	if err != nil || len(provenance) == 0 || provenance[0] != '{' {
		return raw, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	raw.Provenance = provenance
	return raw, provenance, nil
}

func (s Service) Reconcile(ctx context.Context, who auth.Identity, tenantID uuid.UUID, request ReconcileRequest) (Record, error) {
	var out Record
	request, provenance, err := normalize(request)
	if err != nil {
		return out, err
	}
	if s.DB == nil {
		return out, errors.New("legacy import database unavailable")
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var projectID, versionID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT project_id,verified_version_id FROM photo.upload_sessions
			WHERE tenant_id=$1 AND id=$2 AND principal_type='user' AND principal_id=$3
			AND state='verified' AND expected_sha256=$4 AND verified_version_id IS NOT NULL FOR SHARE`,
			tenantID, request.UploadID, who.UserID, request.SourceSHA256).Scan(&projectID, &versionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("UPLOAD_NOT_VERIFIED", 409)
		}
		if err != nil {
			return err
		}
		if _, err = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		var owns bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a
			ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE v.tenant_id=$1 AND v.id=$2
			AND a.project_id=$3 AND a.deleted_at IS NULL)`, tenantID, versionID, projectID).Scan(&owns); err != nil || !owns {
			if err != nil {
				return err
			}
			return domain.NewError("UPLOAD_NOT_VERIFIED", 409)
		}
		report, err := canonical.Marshal(struct {
			UploadID   uuid.UUID       `json:"upload_id"`
			Provenance json.RawMessage `json:"provenance"`
		}{request.UploadID, provenance})
		if err != nil {
			return err
		}
		id := uuid.New()
		var storedVersion uuid.UUID
		err = tx.QueryRow(ctx, `INSERT INTO photo.legacy_imports(id,tenant_id,source_manifest_hash,source_relative,source_sha256,asset_version_id,report)
			VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,source_manifest_hash,source_relative,source_sha256) DO NOTHING
			RETURNING id,asset_version_id`, id, tenantID, request.PlanHash, request.SourceRelative, request.SourceSHA256, versionID, report).Scan(&out.ID, &storedVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT id,asset_version_id,report->>'upload_id',coalesce(report->'provenance','{}'::jsonb)
				FROM photo.legacy_imports WHERE tenant_id=$1 AND source_manifest_hash=$2 AND source_relative=$3 AND source_sha256=$4`,
				tenantID, request.PlanHash, request.SourceRelative, request.SourceSHA256).Scan(&out.ID, &storedVersion, &out.UploadID, &out.Provenance)
			if err != nil {
				return err
			}
			if storedVersion != versionID || out.UploadID != request.UploadID {
				return domain.NewError("LEGACY_IMPORT_CONFLICT", 409)
			}
		} else if err != nil {
			return err
		} else {
			out.UploadID = request.UploadID
			out.Provenance = provenance
		}
		out.PlanHash = request.PlanHash
		out.SourceRelative = request.SourceRelative
		out.SourceSHA256 = request.SourceSHA256
		out.AssetVersionID = storedVersion
		return nil
	})
	return out, err
}
