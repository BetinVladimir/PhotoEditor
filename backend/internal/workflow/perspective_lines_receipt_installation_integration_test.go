package workflow

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

// Installation authority only; does not stand in for activation/replay tests.
func TestIntegrationPerspectiveLinesReceiptPrivateBoundaries(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("database configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := verifyPerspectiveLinesRecoveryInstallation(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for _, signature := range []string{
		"photo.activate_v29_perspective_lines_pre_receipt(uuid,uuid,jsonb)",
		"photo.v29_perspective_lines_submission_state_pre_receipt(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text)",
	} {
		var callable bool
		if err := pool.QueryRow(ctx, "SELECT coalesce(has_function_privilege(current_user,to_regprocedure($1),'EXECUTE'),false)", signature).Scan(&callable); err != nil || callable {
			t.Fatal("private receipt bypass callable", signature, err)
		}
	}
	var apiCallable bool
	if err := pool.QueryRow(ctx, "SELECT has_function_privilege('photo_api','photo.activate_v29_perspective_lines(uuid,uuid,jsonb)','EXECUTE')").Scan(&apiCallable); err != nil || apiCallable {
		t.Fatal("API role can activate directly", err)
	}
}
