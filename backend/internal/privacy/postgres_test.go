package privacy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

type preferenceQuery func(context.Context, string, ...any) pgx.Row

func (f preferenceQuery) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	return f(ctx, q, args...)
}

type preferenceRow struct{ err error }

func (r preferenceRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int64)) = 9007199254740993
	*(dest[1].(*bool)) = true
	*(dest[2].(*bool)) = false
	*(dest[3].(*time.Time)) = time.Unix(1793491200, 0)
	return nil
}
func TestPostgresPreferencesBindsAuthenticatedIdentityAndNullableSelection(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New(), Epoch: 7}
	yes := true
	ctx := context.Background()
	service := PostgresPreferences{DB: preferenceQuery(func(got context.Context, q string, args ...any) pgx.Row {
		if got != ctx || len(args) != 7 || args[0] != who.UserID || args[1] != who.SessionID || args[2] != int64(7) || args[3] != int64(1) || args[4] != &yes {
			t.Fatal("identity or selection not bound", args)
		}
		if value, ok := args[5].(*bool); !ok || value != nil {
			t.Fatal("omitted marketing changed")
		}
		return preferenceRow{}
	})}
	out, err := service.UpdatePreferences(ctx, who, PreferencesUpdate{Analytics: &yes}, 1, uuid.NewString())
	if err != nil || out.Revision != "9007199254740993" || !out.Analytics || out.Marketing {
		t.Fatal(out, err)
	}
}
func TestPostgresPreferencesMapsDatabaseErrors(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	yes := true
	for _, tc := range []struct {
		message string
		status  int
	}{
		{"BILLING_AUTHORITY_INVALID", 403}, {"PRIVACY_REVISION_CONFLICT", 409}, {"PRIVACY_IDEMPOTENCY_CONFLICT", 409}, {"PRIVACY_INPUT_INVALID", 422}, {"private database details", 503},
	} {
		t.Run(tc.message, func(t *testing.T) {
			service := PostgresPreferences{DB: preferenceQuery(func(context.Context, string, ...any) pgx.Row {
				return preferenceRow{err: &pgconn.PgError{Code: "P0001", Message: tc.message}}
			})}
			_, err := service.UpdatePreferences(context.Background(), who, PreferencesUpdate{Analytics: &yes}, 1, uuid.NewString())
			var de *domain.Error
			if !errors.As(err, &de) || de.HTTPStatus != tc.status {
				t.Fatal(err)
			}
		})
	}
}
func TestPostgresPreferencesCancellationAndInvalidInputDoNotQuery(t *testing.T) {
	service := PostgresPreferences{DB: preferenceQuery(func(context.Context, string, ...any) pgx.Row { t.Fatal("unexpected query"); return nil })}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.UpdatePreferences(ctx, auth.Identity{}, PreferencesUpdate{}, 0, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := service.UpdatePreferences(context.Background(), auth.Identity{}, PreferencesUpdate{}, 0, ""); err == nil {
		t.Fatal("invalid input accepted")
	}
}
