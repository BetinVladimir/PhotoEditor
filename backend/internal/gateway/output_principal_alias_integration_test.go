package gateway

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/database"
	"strings"
	"testing"
	"time"
)

// Opt-in native regression: a real restricted gateway LOGIN must evaluate its
// principal guard, rather than fail with42702 because the SQL alias shadows a row.
func TestIntegrationOutputPrincipalGuardRejectsMissingTaskWithoutAliasAmbiguity(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_GATEWAY_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated native gateway LOGIN database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal("invalid isolated gateway configuration")
	}
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["role"] = "photo_gateway"
	database.GuardApplicationRole(cfg)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("isolated gateway connection unavailable")
	}
	defer pool.Close()
	if database.RequireApplicationRole(ctx, pool, "photo_gateway") != nil {
		t.Fatal("restricted gateway role required")
	}
	var name string
	if pool.QueryRow(ctx, "SELECT current_database()").Scan(&name) != nil || !strings.HasPrefix(name, "photoeditor_gateway_output_alias_") {
		t.Fatal("dedicated isolated alias regression database required")
	}
	for _, which := range []string{"begin", "complete"} {
		t.Run(which, func(t *testing.T) {
			task, worker := uuid.New(), uuid.New()
			var e error
			if which == "begin" {
				_, e = pool.Exec(ctx, "SELECT * FROM photo.begin_output($1,1,1,$2,$3,$4,'verified',2,$5,'application/json','photo')", task, worker, strings.Repeat("0", 64), uuid.New(), strings.Repeat("a", 64))
			} else {
				_, e = pool.Exec(ctx, "SELECT * FROM photo.complete_output($1,1,1,$2,$3,$4,'uncommitted-regression-version',2,$5,'application/json')", task, worker, strings.Repeat("0", 64), uuid.New(), strings.Repeat("a", 64))
			}
			var pgerr *pgconn.PgError
			if !errors.As(e, &pgerr) || pgerr.Code != "42501" || pgerr.Message != "AUTHORED_MASK_EXECUTION_PRINCIPAL" {
				t.Fatal("principal guard did not reject missing task with its canonical authorization error")
			}
		})
	}
}
