package collection

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/tenant"
)

// GuestUpload exposes review metadata, never multipart IDs, object keys,
// credentials or the guest principal's session identity.
type GuestUpload struct {
	ID uuid.UUID `json:"id"`
	CollectionID uuid.UUID `json:"collection_id"`
	ShareID uuid.UUID `json:"share_id"`
	Filename string `json:"filename"`
	State string `json:"state"`
	Revision string `json:"revision"`
	ExpectedSize int64 `json:"expected_size"`
	ActualSize *int64 `json:"actual_size,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ItemID *uuid.UUID `json:"item_id,omitempty"`
	AssetVersionID *uuid.UUID `json:"asset_version_id,omitempty"`
	Moderation *string `json:"moderation_state,omitempty"`
}

// ListGuestUploads uses the same Editor boundary as upload.Get/Moderate.
// Upload state has its own revision; collection revision is not a snapshot of
// concurrent upload progress. Each page is an authorized current observation.
func (s Service) ListGuestUploads(ctx context.Context, who auth.Identity, tid, collectionID uuid.UUID, limit int, raw string) (pagination.Page[GuestUpload], error) {
	out := pagination.Page[GuestUpload]{Items: []GuestUpload{}}
	if limit < 1 || limit > 100 { return out, domain.NewError("VALIDATION_FAILED", 422) }
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: collectionID.String(), Sort: "guest-upload:created_at,id"}
	var afterID *uuid.UUID
	var afterTime *time.Time
	if raw != "" {
		cursor, err := s.Cursors.Decode(raw, scope)
		if err != nil { return out, domain.NewError("INVALID_CURSOR", 400) }
		id, err := uuid.Parse(cursor.LastID)
		if err != nil || id == uuid.Nil { return out, domain.NewError("INVALID_CURSOR", 400) }
		stamp, err := time.Parse(time.RFC3339Nano, cursor.LastPosition)
		if err != nil { return out, domain.NewError("INVALID_CURSOR", 400) }
		afterID, afterTime = &id, &stamp
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if _, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, false); err != nil { return err }
		rows, err := tx.Query(ctx, `SELECT u.id,u.collection_id,u.share_id,u.filename,u.state,u.revision::text,
 u.expected_size,u.actual_size,u.created_at,i.id,i.asset_version_id,i.moderation_state
 FROM photo.upload_sessions u
 LEFT JOIN photo.ingest_upload_bindings b ON b.tenant_id=u.tenant_id AND b.upload_id=u.id AND b.run_id=u.ingest_run_id
 LEFT JOIN photo.collection_items i ON i.tenant_id=b.tenant_id AND i.id=b.item_id AND i.collection_id=u.collection_id
 WHERE u.tenant_id=$1 AND u.collection_id=$2 AND u.principal_type='guest' AND u.share_id IS NOT NULL
 AND ($3::uuid IS NULL OR (u.created_at,u.id)>($4::timestamptz,$3::uuid))
 ORDER BY u.created_at,u.id LIMIT $5`, tid, collectionID, afterID, afterTime, limit+1)
		if err != nil { return err }
		defer rows.Close()
		for rows.Next() {
			var item GuestUpload
			if err := rows.Scan(&item.ID,&item.CollectionID,&item.ShareID,&item.Filename,&item.State,&item.Revision,&item.ExpectedSize,&item.ActualSize,&item.CreatedAt,&item.ItemID,&item.AssetVersionID,&item.Moderation); err != nil { return err }
			out.Items = append(out.Items, item)
		}
		if err := rows.Err(); err != nil { return err }
		rows.Close()
		role, err = tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor)
		if err != nil { return err }
		_,err=authorize(ctx,tx,who,tid,collectionID,role,domain.Editor,false)
		return err
	})
	if err != nil { return out, err }
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		scope.LastID, scope.LastPosition = last.ID.String(), last.CreatedAt.UTC().Format(time.RFC3339Nano)
		next, err := s.Cursors.Encode(scope)
		if err != nil { return out, err }
		out.NextCursor = &next
	}
	return out, nil
}
