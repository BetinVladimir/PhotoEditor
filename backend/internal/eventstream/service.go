package eventstream

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
)

type Service struct{ DB *pgxpool.Pool }
type Event struct {
	Cursor       string          `json:"cursor"`
	Type         string          `json:"type"`
	RunID        *uuid.UUID      `json:"run_id"`
	CollectionID *uuid.UUID      `json:"collection_id"`
	Payload      json.RawMessage `json:"payload"`
	CreatedAt    time.Time       `json:"created_at"`
}
type Page struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"next_cursor"`
}

func (s Service) List(ctx context.Context, who auth.Identity, tid uuid.UUID, afterRaw string, limit int) (Page, error) {
	out := Page{Items: []Event{}, NextCursor: afterRaw}
	if limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var after int64
	if afterRaw != "" {
		value, err := strconv.ParseInt(afterRaw, 10, 64)
		if err != nil || value < 0 {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		after = value
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var minimum *int64
		if err := tx.QueryRow(ctx, `SELECT min(cursor) FROM photo.run_events WHERE tenant_id=$1`, tid).Scan(&minimum); err != nil {
			return err
		}
		if after > 0 && minimum != nil && after < *minimum-1 {
			return domain.NewError("EVENTS_EXPIRED", 410)
		}
		rows, err := tx.Query(ctx, `SELECT e.cursor,e.type,e.run_id,e.collection_id,e.payload,e.created_at FROM photo.run_events e WHERE e.tenant_id=$1 AND e.cursor>$2 AND ((e.audience='principal' AND e.principal_id=$3) OR (e.audience='tenant' AND $4::boolean) OR (e.collection_id IS NOT NULL AND EXISTS(SELECT FROM photo.collections c JOIN photo.projects p ON p.tenant_id=c.tenant_id AND p.id=c.project_id WHERE c.tenant_id=e.tenant_id AND c.id=e.collection_id AND c.deleted_at IS NULL AND p.archived_at IS NULL AND ($4::boolean OR p.visibility='workspace' OR EXISTS(SELECT FROM photo.project_acl a WHERE a.tenant_id=p.tenant_id AND a.project_id=p.id AND a.user_id=$3))))) ORDER BY e.cursor LIMIT $5`, tid, after, who.UserID, role.Allows(domain.Admin), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item Event
			var cursor int64
			if err = rows.Scan(&cursor, &item.Type, &item.RunID, &item.CollectionID, &item.Payload, &item.CreatedAt); err != nil {
				return err
			}
			item.Cursor = strconv.FormatInt(cursor, 10)
			out.NextCursor = item.Cursor
			out.Items = append(out.Items, item)
		}
		return rows.Err()
	})
	return out, err
}
