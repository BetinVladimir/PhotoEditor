// Isolated fixture driver only: real normal Recovery and/or ApplyCoordinator.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/workflow"
	"strings"
	"time"
)

var currentPhase = "CONFIG"

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(os.Getenv("APPLY_FAILURE_TEST_DSN"))
	if e != nil {
		return errors.New("invalid isolated DSN")
	}
	if !strings.HasPrefix(cfg.ConnConfig.Database, "photoeditor_apply_failure2655_") {
		return errors.New("MAIN forbidden")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	cfg.MaxConns = 1
	database.GuardApplicationRole(cfg)
	currentPhase = "POOL"
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return e
	}
	defer p.Close()
	currentPhase = "ROLE"
	if e = database.RequireApplicationRole(ctx, p, "photo_scheduler"); e != nil {
		return e
	}
	mode := os.Getenv("APPLY_FAILURE_TEST_COMPONENT")
	out := map[string]any{"database_is_owned_clone": true, "role": "photo_scheduler"}
	switch mode {
	case "recovery":
		currentPhase = "RECOVERY"
		n, e := (workflow.Recovery{DB: p}).Recover(ctx, 1)
		if e != nil {
			return e
		}
		out["normal_recovery_count"] = n
	case "coordinator":
		currentPhase = "COORDINATOR"
		worked, e := (workflow.ApplyCoordinator{Store: workflow.ApplyStore{DB: diagnosticDB{Pool: p, phase: &currentPhase}}}).ProcessOne(ctx)
		if e != nil {
			return e
		}
		out["worked"] = worked
	default:
		return errors.New("closed normal component required")
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}
func main() {
	if e := run(); e != nil {
		_ = json.NewEncoder(os.Stderr).Encode(safeFailure(currentPhase, e))
		os.Exit(1)
	}
}
