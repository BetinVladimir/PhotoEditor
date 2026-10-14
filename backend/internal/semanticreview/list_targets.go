package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/tenant"
	"time"
)

type TargetReader struct {
	DB      *pgxpool.Pool
	Cursors pagination.Codec
}

func (s TargetReader) List(ctx context.Context, who auth.Identity, tid, intent uuid.UUID, limit int, raw string) (pagination.Page[TargetResource], error) {
	out := pagination.Page[TargetResource]{Items: []TargetResource{}}
	if tid == uuid.Nil || intent == uuid.Nil || limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	hash, err := canonical.Hash(map[string]any{"intent_id": intent.String()})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: hash, Sort: "semantic_target:created_at,id"}
	var afterID *uuid.UUID
	var afterTime *time.Time
	if raw != "" {
		cursor, e := s.Cursors.Decode(raw, scope)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, e := uuid.Parse(cursor.LastID)
		if e != nil || id == uuid.Nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		at, e := time.Parse(time.RFC3339Nano, cursor.LastPosition)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterID = &id
		afterTime = &at
	}
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var allowed bool
		e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.semantic_intents i JOIN photo.projects p ON p.tenant_id=i.tenant_id AND p.id=i.project_id WHERE i.tenant_id=$1 AND i.id=$2 AND p.archived_at IS NULL AND ($3::boolean OR p.visibility='workspace' OR EXISTS(SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p.tenant_id AND acl.project_id=p.id AND acl.user_id=$4)))`, tid, intent, role.Allows(domain.Admin), who.UserID).Scan(&allowed)
		if e != nil {
			return e
		}
		if !allowed {
			return domain.NewError("NOT_FOUND", 404)
		}
		rows, e := tx.Query(ctx, `SELECT t.id,t.tenant_id,t.project_id,t.created_at,t.revision,t.updated_at,t.intent_id,t.child_run_id,t.batch_id,t.asset_version_id,t.candidate_artifact_id,t.review_id,t.override_id,t.state,a.sha256 FROM photo.semantic_targets t LEFT JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.candidate_artifact_id AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) WHERE t.tenant_id=$1 AND t.intent_id=$2 AND ($3::uuid IS NULL OR (t.created_at,t.id)>($4::timestamptz,$3::uuid)) ORDER BY t.created_at,t.id LIMIT $5`, tid, intent, afterID, afterTime, limit+1)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var row TargetRow
			var sha *string
			if e = rows.Scan(&row.ID, &row.TenantID, &row.ProjectID, &row.CreatedAt, &row.Revision, &row.UpdatedAt, &row.IntentID, &row.ChildRunID, &row.BatchID, &row.AssetVersionID, &row.CandidateArtifactID, &row.ReviewID, &row.OverrideID, &row.State, &sha); e != nil {
				return e
			}
			var document *TargetDocument
			if sha != nil && row.CandidateArtifactID != nil {
				document = &TargetDocument{*row.CandidateArtifactID, *sha}
			}
			resource, e := NewTargetResource(row, document)
			if e != nil {
				return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
			}
			out.Items = append(out.Items, resource)
		}
		return rows.Err()
	})
	if err != nil {
		return pagination.Page[TargetResource]{Items: []TargetResource{}}, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		scope.LastID = last.ID.String()
		scope.LastPosition = last.Data.CreatedAt.Format(time.RFC3339Nano)
		cursor, e := s.Cursors.Encode(scope)
		if e != nil {
			return pagination.Page[TargetResource]{Items: []TargetResource{}}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		out.NextCursor = &cursor
	}
	return out, nil
}
