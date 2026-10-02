package v2draft

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2resource"
)

type Page struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}

// History pages immutable recipe versions of the current accepted asset
// version. Its signed cursor cannot be reused for another actor or asset.
func (s Service) History(ctx context.Context, who auth.Identity, tenantID,
	assetID uuid.UUID, pageLimit int, rawCursor string) (Page, error) {
	out := Page{Items: []json.RawMessage{}}
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if assetID == uuid.Nil || pageLimit < 1 || pageLimit > 100 || len(rawCursor) > 2048 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	filter, err := canonical.Hash(struct {
		AssetID uuid.UUID `json:"asset_id"`
	}{assetID})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tenantID.String(), Principal: who.UserID.String(),
		FilterHash: filter, Sort: "v2:document_history:version_no,id"}
	lastVersion := int64(math.MaxInt64)
	var lastID uuid.UUID
	for i := range lastID {
		lastID[i] = 0xff
	}
	if rawCursor != "" {
		cursor, e := s.Cursors.Decode(rawCursor, scope)
		if e != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		lastVersion, e = strconv.ParseInt(cursor.LastPosition, 10, 64)
		if e != nil || lastVersion < 1 {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		lastID, e = uuid.Parse(cursor.LastID)
		if e != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	type position struct {
		id      uuid.UUID
		version int64
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer,
		func(tx pgx.Tx, role domain.Role) error {
			streamID, e := currentDraftStream(ctx, tx, who, tenantID, assetID, role)
			if e != nil {
				return e
			}
			rows, e := tx.Query(ctx, `SELECT id,version_no FROM photo.v2_documents
			 WHERE tenant_id=$1 AND stream_id=$2
			 AND document_type='EditingRecipeV3'
			 AND (version_no,id)<($3,$4) ORDER BY version_no DESC,id DESC LIMIT $5`,
				tenantID, streamID, lastVersion, lastID, pageLimit+1)
			if e != nil {
				return e
			}
			positions := make([]position, 0, pageLimit+1)
			for rows.Next() {
				var p position
				if e = rows.Scan(&p.id, &p.version); e != nil {
					rows.Close()
					return e
				}
				positions = append(positions, p)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			hasNext := len(positions) > pageLimit
			if hasNext {
				positions = positions[:pageLimit]
			}
			for _, p := range positions {
				body, e := v2resource.Load(ctx, tx, tenantID, p.id, "v2_documents")
				if e != nil {
					return e
				}
				out.Items = append(out.Items, body)
			}
			if hasNext {
				last := positions[len(positions)-1]
				scope.LastPosition = strconv.FormatInt(last.version, 10)
				scope.LastID = last.id.String()
				next, e := s.Cursors.Encode(scope)
				if e != nil {
					return e
				}
				if len(next) > 2048 {
					return domain.NewError("CURSOR_UNAVAILABLE", 503)
				}
				out.NextCursor = &next
			}
			return nil
		})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "42P01" {
		return Page{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return out, err
}
