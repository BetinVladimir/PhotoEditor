package workflow

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise installed SQL capabilities under actual separate service roles.
// This verifies startup authority, not producer inference or parent acceptance.
func TestIntegrationNativeRecoveryInstallationRoles(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	open := func(role string) *pgxpool.Pool {
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			t.Fatal("database configuration invalid")
		}
		cfg.ConnConfig.RuntimeParams["role"] = role
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal("database role pool unavailable")
		}
		t.Cleanup(pool.Close)
		var actual string
		if err := pool.QueryRow(ctx, "SELECT current_user").Scan(&actual); err != nil || actual != role {
			t.Fatal("service role unavailable", role, err)
		}
		return pool
	}
	scheduler, api := open("photo_scheduler"), open("photo_api")
	if err := verifyNativeRecoveryInstallation(ctx, scheduler); err != nil {
		t.Fatal("scheduler installation", err)
	}
	if err := verifyNativeRecoverySourceInstallation(ctx, api); err != nil {
		t.Fatal("source installation", err)
	}
	if err := verifyNativeRecoveryInstallation(ctx, api); err == nil {
		t.Fatal("source role accepted scheduler installation")
	}
	if err := verifyNativeRecoverySourceInstallation(ctx, scheduler); err == nil {
		t.Fatal("scheduler role accepted source installation")
	}
}
