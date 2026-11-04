package privacy

import (
	"context"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
)

// PreferenceDatabase is the auth-role database, never the API or maintenance pool.
type PreferenceDatabase interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresPreferences struct{ DB PreferenceDatabase }

func (s PostgresPreferences) UpdatePreferences(ctx context.Context, who auth.Identity, req PreferencesUpdate, revision int64, key string) (Preferences, error) {
	if err := ctx.Err(); err != nil {
		return Preferences{}, err
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || who.Epoch < 0 {
		return Preferences{}, domain.NewError("FORBIDDEN", 403)
	}
	if req.Validate() != nil || revision < 1 || !idempotency.ValidKey(key) {
		return Preferences{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if s.DB == nil {
		return Preferences{}, domain.NewError("SERVICE_UNAVAILABLE", 503)
	}
	var out Preferences
	var currentRevision int64
	err := s.DB.QueryRow(ctx, `SELECT revision,analytics,marketing,updated_at FROM photo.account_update_privacy_preferences($1,$2,$3,$4,$5,$6,$7,NULL)`, who.UserID, who.SessionID, who.Epoch, revision, req.Analytics, req.Marketing, key).Scan(&currentRevision, &out.Analytics, &out.Marketing, &out.UpdatedAt)
	if err != nil {
		if ctx.Err() != nil {
			return Preferences{}, ctx.Err()
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			switch pg.Message {
			case "BILLING_AUTHORITY_INVALID":
				return Preferences{}, domain.NewError("FORBIDDEN", 403)
			case "PRIVACY_REVISION_CONFLICT":
				return Preferences{}, domain.NewError("REVISION_CONFLICT", 409)
			case "PRIVACY_IDEMPOTENCY_CONFLICT":
				return Preferences{}, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
			case "PRIVACY_INPUT_INVALID":
				return Preferences{}, domain.NewError("VALIDATION_FAILED", 422)
			}
		}
		return Preferences{}, domain.NewError("SERVICE_UNAVAILABLE", 503)
	}
	out.Revision = strconv.FormatInt(currentRevision, 10)
	return out, nil
}
