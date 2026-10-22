package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/workflow"
	"regexp"
	"time"
)

func run() error {
	ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
	defer c()
	var ids []uuid.UUID
	if json.Unmarshal([]byte(os.Getenv("EXPECTED_RUN_IDS")), &ids) != nil || len(ids) != 17 {
		return errors.New("exact17scope required")
	}
	expected := map[uuid.UUID]bool{uuid.MustParse("13dce0c7-d888-4bf1-a550-99a9e1cef6d5"): true, uuid.MustParse("20524e52-45d3-4a46-b01f-026723b6399a"): true, uuid.MustParse("28d2cb68-3fb8-4f20-b03e-2974000059a8"): true, uuid.MustParse("39858245-cbe6-4301-a90d-7023832c33ee"): true, uuid.MustParse("43cce698-ed34-4d6e-9eb6-079fddc84cb5"): true, uuid.MustParse("4a878428-0f40-49cb-a952-91aec2c311ab"): true, uuid.MustParse("5b0e26a8-fefb-418c-88c9-c6f5d2b35076"): true, uuid.MustParse("642512e3-3034-451c-b57c-684801ba3315"): true, uuid.MustParse("70ff3661-0943-47cd-a252-ed27a347c915"): true, uuid.MustParse("86b8d286-ab54-420b-8fa9-ae22be5c2310"): true, uuid.MustParse("a342cc3c-6cce-4301-a783-9af9bc5f4c6b"): true, uuid.MustParse("a6892817-2885-431e-a434-822d0d63157f"): true, uuid.MustParse("ab8d1436-216e-476f-9d82-30f7373603ff"): true, uuid.MustParse("b9c9782b-6f04-4cc1-96d9-d71a3e23bcfa"): true, uuid.MustParse("c1458235-1300-45bf-8a57-84ec9ae715e5"): true, uuid.MustParse("e1489ed8-91d3-4f47-8ead-9fbf22d881dc"): true, uuid.MustParse("efd62270-fc7d-4f99-8687-5f4801f29da1"): true}
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || seen[id] || !expected[id] {
			return errors.New("unexpected recovery scope")
		}
		seen[id] = true
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(os.Getenv("EXPECTED_RECOVERY_PREFLIGHT_SHA256")) {
		return errors.New("outer scope receipt required")
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
	var permitted bool
	if e = p.QueryRow(ctx, "select bool_and(has_function_privilege(current_user,p.oid,'EXECUTE')) from pg_proc p where p.oid=any(array['photo.cancel_idle_task()'::regprocedure,'photo.pending_task_failures(integer)'::regprocedure,'photo.activate_task_retries(integer)'::regprocedure])").Scan(&permitted); e != nil || !permitted {
		return errors.New("scheduler recovery interface unavailable")
	}
	if os.Getenv("RECOVERY_PREFLIGHT_ONLY") == "true" {
		fmt.Println(`{"status":"EXACT17_RESTRICTED_SCHEDULER_PREFLIGHT_PASS","recovery_executed":false}`)
		return nil
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
		metadata := map[string]string{"status": "RECOVERY_FAILED", "error_class": fmt.Sprintf("%T", e)}
		var pg *pgconn.PgError
		if errors.As(e, &pg) {
			if regexp.MustCompile(`^[0-9A-Z]{5}$`).MatchString(pg.Code) {
				metadata["sqlstate"] = pg.Code
			}
			if regexp.MustCompile(`^[A-Za-z0-9_.]{1,160}$`).MatchString(pg.Routine) {
				metadata["routine"] = pg.Routine
			}
		}
		_ = json.NewEncoder(os.Stderr).Encode(metadata)
		os.Exit(1)
	}
}
