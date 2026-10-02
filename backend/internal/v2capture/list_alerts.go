package v2capture

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type AlertPage struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}

func (s Service) ListAlerts(ctx context.Context, who auth.Identity, tenantID, sessionID uuid.UUID,
	rawCursor string, limit int) (AlertPage, error) {
	out := AlertPage{Items: []json.RawMessage{}}
	if sessionID == uuid.Nil || limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	filter, e := canonical.Hash(map[string]string{"session_id": sessionID.String()})
	if e != nil {
		return out, e
	}
	scope := pagination.Cursor{Tenant: tenantID.String(), Principal: who.UserID.String(), FilterHash: filter, Sort: "capture-alerts-created-id-desc"}
	var afterTime *time.Time
	var afterID *uuid.UUID
	if rawCursor != "" {
		cursor, e := s.Cursors.Decode(rawCursor, scope)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		timestamp, e := time.Parse(time.RFC3339Nano, cursor.LastPosition)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, e := uuid.Parse(cursor.LastID)
		if e != nil || id == uuid.Nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterTime, afterID = &timestamp, &id
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id FROM photo.shoot_sessions WHERE tenant_id=$1 AND id=$2`, tenantID, sessionID).Scan(&projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Viewer, false); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT a.id,a.created_at,(to_jsonb(a)-ARRAY['technical_quality','quality_metric','quality_report_sha'])||jsonb_build_object('resource_type','capture_alerts','revision',a.revision::text,'confirmation_id',c.id)
   FROM photo.capture_alerts a LEFT JOIN photo.capture_frame_confirmations c
   ON c.tenant_id=a.tenant_id AND c.project_id=a.project_id AND c.session_id=a.session_id
   AND c.receipt_id=a.receipt_id AND c.alert_id=a.id AND c.asset_version_id=a.asset_version_id
   WHERE a.tenant_id=$1 AND a.project_id=$2 AND a.session_id=$3
   AND ($4::timestamptz IS NULL OR (a.created_at,a.id)<($4,$5::uuid))
   ORDER BY a.created_at DESC,a.id DESC LIMIT $6`, tenantID, projectID, sessionID, afterTime, afterID, limit+1)
		if e != nil {
			return e
		}
		defer rows.Close()
		var lastID uuid.UUID
		var lastTime time.Time
		for rows.Next() {
			var id uuid.UUID
			var created time.Time
			var body json.RawMessage
			if e = rows.Scan(&id, &created, &body); e != nil {
				return e
			}
			if len(out.Items) == limit {
				scope.LastID, scope.LastPosition = lastID.String(), lastTime.UTC().Format(time.RFC3339Nano)
				next, e := s.Cursors.Encode(scope)
				if e != nil {
					return e
				}
				out.NextCursor = &next
				break
			}
			out.Items = append(out.Items, body)
			lastID, lastTime = id, created
		}
		return rows.Err()
	})
	return out, mutationError(err)
}
