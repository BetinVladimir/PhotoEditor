package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	retryscope "photoeditor/backend/_work/user-raw-retry-scope-r2"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/upload"
	"time"
)

type scopedLeases struct {
	upload.SQLCompletionLeases
	expected map[uuid.UUID]retryscope.Upload
	pool     *pgxpool.Pool
}

func (s scopedLeases) Claim(ctx context.Context) (*upload.CompletionLease, error) {
	return retryscope.CheckedClaim(ctx, func(c context.Context) (retryscope.Transaction, error) { return s.pool.Begin(c) }, func(t retryscope.Transaction) (*upload.CompletionLease, error) {
		return (upload.SQLCompletionLeases{DB: t.(pgx.Tx), Bucket: s.SQLCompletionLeases.Bucket}).Claim(ctx)
	}, func(l *upload.CompletionLease) bool {
		x, ok := s.expected[l.UploadID]
		return ok && l.TenantID == x.TenantID && l.RunID == x.RunID && l.Filename == x.File && l.ExpectedBytes == x.Bytes && l.ExpectedSHA != nil && *l.ExpectedSHA == x.SHA
	})
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	allowed, _, err := retryscope.Scope()
	if err != nil {
		return err
	}
	if os.Getenv("CONFIRM_EXACT_TWO_COMPLETION") != "reviewed-two-originals" {
		return errors.New("root review required")
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("database config invalid")
	}
	cfg.MaxConns = 2
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	database.GuardApplicationRole(cfg)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = database.RequireApplicationRole(ctx, pool, "photo_scheduler"); err != nil {
		return err
	}
	config := objectstore.ConfigFromEnv(true)
	store, err := objectstore.New(config)
	if err != nil {
		return err
	}
	var bindings []retryscope.Upload
	if json.Unmarshal([]byte(os.Getenv("EXPECTED_NEW_UPLOAD_SCOPE")), &bindings) != nil {
		return errors.New("bad reviewed upload bindings")
	}
	expected := map[uuid.UUID]retryscope.Upload{}
	for _, b := range bindings {
		expected[b.UploadID] = b
	}
	processor := upload.CompletionProcessor{Leases: scopedLeases{upload.SQLCompletionLeases{DB: pool, Bucket: config.Bucket}, expected, pool}, Objects: store}
	completed := 0
	for range allowed {
		ok, err := processor.ProcessOne(ctx)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		completed++
	}
	fmt.Printf("{\"status\":\"ACTUAL_UPLOAD_COMPLETION_COMPONENT_PASS\",\"completed\":%d,\"scheduler_started\":false}\n", completed)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "COMPLETION_FAILURE_CLASS=%T\n", err)
		os.Exit(1)
	}
}
