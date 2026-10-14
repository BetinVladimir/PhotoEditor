package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSigningReplaySQLPreflightRefusesMissingDatabaseAndCancellation(t *testing.T) {
	if err := verifySigningRuntimeSQL(context.Background(), nil); err == nil {
		t.Fatal("missing signing replay database accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifySigningRuntimeSQL(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled signing replay preflight reached database", err)
	}
}

func TestIntegrationSigningAdmissionSQLPreflight(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated database migrated through V31 runtime diagnostics required")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	if err := verifySigningRuntimeSQL(ctx, api); err != nil {
		t.Fatal("configured signer cannot prove runtime SQL installation", err)
	}
}
