package workflow

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestV30RuntimeStartupRequiresCurrentAdmissionABI(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated database migrated through V30 runtime gates required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE photo_scheduler")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := validateInterchangeImportRuntimeSQL(ctx, pool); err != nil {
		t.Fatal("import admission ABI unavailable", err)
	}
	if err := validateInterchangeExportRuntimeSQL(ctx, pool); err != nil {
		t.Fatal("export admission ABI unavailable", err)
	}
	if err := validateDeliveryRuntimeSQL(ctx, pool); err != nil {
		t.Fatal("delivery admission ABI unavailable", err)
	}
}
