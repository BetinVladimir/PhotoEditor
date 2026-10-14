package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"time"
)

type SQLLocalPortraitOwnerScope struct{ DB *pgxpool.Pool }

// Check is preflight only. Durable creation must repeat the predicate under
// its tenant/run/snapshot locks; successful preflight grants no publication IO.
func (s SQLLocalPortraitOwnerScope) Check(ctx context.Context, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(raw) < 2 || len(raw) > 1048576 {
		return v2input.ErrInvalid
	}
	var request LocalPortraitOwnerRequest
	if json.Unmarshal(raw, &request) != nil || request.SchemaVersion != 1 {
		return v2input.ErrInvalid
	}
	body, err := canonical.Marshal(request)
	if err != nil || !bytes.Equal(body, raw) || s.DB == nil {
		return v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return database.InTransaction(bounded, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var ready bool
		if err := tx.QueryRow(bounded, "SELECT photo.v28_local_owner_request_scope($1::jsonb)", string(body)).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return v2input.ErrExpired
		}
		return bounded.Err()
	})
}
