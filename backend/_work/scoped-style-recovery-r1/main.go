package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/workflow"
	"time"
)

func run() error {
	ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
	defer c()
	var ids []uuid.UUID
	if json.Unmarshal([]byte(os.Getenv("EXPECTED_RUN_IDS")), &ids) != nil || len(ids) != 17 {
		return errors.New("exact17scope required")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if e != nil {
		return errors.New("database config")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	database.GuardApplicationRole(cfg)
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return errors.New("database connect")
	}
	defer p.Close()
	if e = database.RequireApplicationRole(ctx, p, "photo_scheduler"); e != nil {
		return e
	}
	var outside int
	if e = p.QueryRow(ctx, "select count(*) from photo.tasks where state in ('queued','running','retry_wait') and not(run_id=any($1::uuid[]))", ids).Scan(&outside); e != nil {
		return e
	}
	if outside != 0 {
		return errors.New("outside active task scope")
	}
	n, e := (workflow.Recovery{DB: p}).Recover(ctx, 100)
	if e != nil {
		return e
	}
	fmt.Printf("{\"status\":\"EXACT17_STANDARD_STYLE_RECOVERY_EXECUTED\",\"count\":%d,\"manual_resets\":false}\n", n)
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
