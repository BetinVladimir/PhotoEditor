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

// Audience tags are disjoint. A collection reference on a private event does
// not turn it into a project-visible broadcast. Reuse this predicate for both
// retention and page reads so another principal cannot affect recovery.
const visibleEventSQL = `((e.audience='principal' AND e.principal_id=$2) OR (e.audience='tenant' AND $3::boolean) OR (e.audience='collection' AND e.collection_id IS NOT NULL AND EXISTS(SELECT FROM photo.collections c JOIN photo.projects p ON p.tenant_id=c.tenant_id AND p.id=c.project_id WHERE c.tenant_id=e.tenant_id AND c.id=e.collection_id AND c.deleted_at IS NULL AND p.archived_at IS NULL AND ($3::boolean OR p.visibility='workspace' OR EXISTS(SELECT FROM photo.project_acl a WHERE a.tenant_id=p.tenant_id AND a.project_id=p.id AND a.user_id=$2)))))`

func (s Service) List(ctx context.Context, who auth.Identity, tid uuid.UUID, afterRaw string, limit int) (Page, error) {
	out := Page{Items: []Event{}, NextCursor: afterRaw}
	if limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var after int64
	if afterRaw != "" {
		value, err := strconv.ParseInt(afterRaw, 10, 64)
		if err != nil || value < 0 || strconv.FormatInt(value,10) != afterRaw {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		after = value
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var minimum *int64
		if err := tx.QueryRow(ctx, `SELECT min(e.cursor) FROM photo.run_events e WHERE e.tenant_id=$1 AND `+visibleEventSQL, tid, who.UserID, role.Allows(domain.Admin)).Scan(&minimum); err != nil {
			return err
		}
        // The minimum query can wait on maintenance DDL. Do not apply even
        // retention bounds with a stale wider audience after demotion.
        afterMinimumRole,err:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Viewer)
        if err!=nil{return err}
        if role.Allows(domain.Admin)!=afterMinimumRole.Allows(domain.Admin){return domain.NewError("NOT_FOUND",404)}
		if after > 0 && minimum != nil && after < *minimum-1 {
			return domain.NewError("EVENTS_EXPIRED", 410)
		}
		rows, err := tx.Query(ctx, `SELECT e.cursor,e.type,e.run_id,e.collection_id,e.payload,e.created_at FROM photo.run_events e WHERE e.tenant_id=$1 AND e.cursor>$4 AND `+visibleEventSQL+` ORDER BY e.cursor LIMIT $5`, tid, who.UserID, role.Allows(domain.Admin), after, limit)
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
		if err=rows.Err();err!=nil{return err}
        rows.Close()
        // Tenant-wide events require the final role too, not a cached Admin
        // from before the two page queries.
        currentRole,err:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Viewer)
        if err!=nil{return err}
        if role.Allows(domain.Admin)!=currentRole.Allows(domain.Admin){return domain.NewError("NOT_FOUND",404)}
        return nil
	})
	return out, err
}
