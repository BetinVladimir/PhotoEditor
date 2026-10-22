// Explicit privileged diagnostic context, normal scheduler functions, no shadow SQL.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/workflow"
	"strings"
	"syscall"
	"time"
)

const scopePath = "/Users/freelancer/Documents/PhotoEditor/work/rtx-initial-media-audit-r1/derived68-task-scope.json"

// The exact expected-ID receipt is review-pinned separately; not evidence that rows exist.
func one(ctx context.Context, tx pgx.Tx, s scope, component string) (int, error) {
	if e := outsideZero(ctx, tx, s); e != nil {
		return 0, e
	}
	before, e := capture(ctx, tx, s)
	if e != nil {
		return 0, e
	}
	if _, e = tx.Exec(ctx, `SET LOCAL ROLE photo_scheduler`); e != nil {
		return 0, e
	}
	var role string
	if e = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); e != nil || role != "photo_scheduler" {
		return 0, errors.New("scheduler role required")
	}
	count := 0
	switch component {
	case "dispatch":
		_, changed, err := (workflow.Dispatcher{}).DispatchOneInTransaction(ctx, tx, s.selected)
		e = err
		if changed {
			count = 1
		}
	case "recover":
		count, e = (workflow.Recovery{DB: tx}).Recover(ctx, 1)
	case "finalize":
		var changed bool
		changed, e = (workflow.MediaRetryFinalizer{DB: tx}).ProcessOne(ctx)
		if changed {
			count = 1
		}
	default:
		return 0, errors.New("unknown component")
	}
	if e != nil {
		return 0, e
	}
	if _, e = tx.Exec(ctx, `RESET ROLE`); e != nil {
		return 0, e
	}
	after, e := capture(ctx, tx, s)
	if e != nil {
		return 0, e
	}
	if !equalSnapshots(before, after) {
		return 0, errors.New("protected outside state changed; rollback required")
	}
	return count, nil
}
func receipt(path, pin, root string) error {
	absolute, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	resolved, e := filepath.EvalSymlinks(absolute)
	if e != nil {
		return e
	}
	if resolved != absolute || !strings.HasPrefix(absolute, root+string(os.PathSeparator)) {
		return errors.New("receipt must be immutable owned work path")
	}
	info, e := os.Stat(absolute)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return errors.New("bounded receipt required")
	}
	raw, e := os.ReadFile(absolute)
	if e != nil {
		return e
	}
	h := sha256.Sum256(raw)
	if len(pin) != 64 || hex.EncodeToString(h[:]) != pin {
		return errors.New("actual reviewed receipt bytes drift")
	}
	return nil
}
func run() error {
	component := flag.String("component", "", "dispatch|recover|finalize, exactly one transaction")
	reviewPath := flag.String("review", "", "root independent execution review")
	pin := flag.String("review-sha256", "", "exact review SHA")
	flag.Parse()
	if *component != "dispatch" && *component != "recover" && *component != "finalize" {
		return errors.New("explicit component required")
	}
	raw, e := os.ReadFile(*reviewPath)
	if e != nil {
		return e
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != *pin || len(*pin) != 64 {
		return errors.New("root review SHA required")
	}
	var review struct {
		WorkerPath         string            `json:"physical_media_worker_evidence_path"`
		QuiescencePath     string            `json:"current_quiescence_receipt_path"`
		ApprovedAt         time.Time         `json:"approved_at"`
		Runtime            string            `json:"runtime_sha256"`
		Policy             string            `json:"capacity_policy_sha256"`
		SourcePins         map[string]string `json:"source_pins"`
		FunctionCatalogSHA string            `json:"function_catalog_sha256"`
		Approved           bool              `json:"approved"`
		Scope              string            `json:"derived_scope_sha256"`
		Worker             string            `json:"physical_media_worker_evidence_sha256"`
		Quiescence         string            `json:"current_quiescence_receipt_sha256"`
		Head               int               `json:"head"`
		Component          string            `json:"component"`
	}
	if json.Unmarshal(raw, &review) != nil || !review.Approved || review.Head != 2650 || len(review.FunctionCatalogSHA) != 64 || review.Component != *component || len(review.Worker) != 64 || len(review.Quiescence) != 64 {
		return errors.New("concrete root review required")
	}
	if time.Since(review.ApprovedAt) > 2*time.Minute || time.Until(review.ApprovedAt) > 5*time.Second || review.Runtime != "7b86f8453af0a07f5061c0f1b6e8dc26d4f100418bdcafc4eb2576a35c8d4c74" || review.Policy != "bd444b18fbcf9c8459b8b7ed3931d7755fd7179b0fca3c4e9674665f0c184506" {
		return errors.New("fresh runtime/policy review required")
	}
	for _, v := range [][2]string{{review.WorkerPath, review.Worker}, {review.QuiescencePath, review.Quiescence}} {
		if e = receipt(v[0], v[1], "/Users/freelancer/Documents/PhotoEditor/work"); e != nil {
			return e
		}
	}
	for _, path := range []string{"backend/_work/initial-media-components-r1/main.go", "backend/_work/initial-media-components-r1/scope.go", "backend/internal/workflow/dispatch.go", "backend/internal/taskrecovery/recovery.go", "backend/internal/workflow/media_retry_store.go"} {
		raw, e := os.ReadFile("/Users/freelancer/Documents/PhotoEditor/" + path)
		if e != nil {
			return e
		}
		h := sha256.Sum256(raw)
		if review.SourcePins[path] != hex.EncodeToString(h[:]) {
			return errors.New("reviewed component source drift")
		}
	}
	raw, e = os.ReadFile(scopePath)
	if e != nil {
		return e
	}
	s, e := parseScope(raw, review.Scope)
	if e != nil {
		return e
	}
	lock, e := os.OpenFile("/Users/freelancer/Documents/PhotoEditor/work/pending-full38-exclusive.lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return errors.New("common heavy lock busy")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	conn, e := pgx.Connect(ctx, os.Getenv("OPERATOR_DATABASE_URL"))
	if e != nil {
		return e
	}
	defer conn.Close(ctx)
	var login string
	var privileged bool
	if e = conn.QueryRow(ctx, `SELECT current_user,rolsuper FROM pg_roles WHERE rolname=current_user`).Scan(&login, &privileged); e != nil || login != "rtx_snapshot_admin" || !privileged {
		return errors.New("actual reviewed operator context required")
	}
	tx, e := conn.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL statement_timeout='15000'; SET LOCAL lock_timeout='3000'`); e != nil {
		return e
	}
	var head int
	if e = tx.QueryRow(ctx, "SELECT max(version_id) FROM public.goose_db_version WHERE is_applied").Scan(&head); e != nil || head != review.Head {
		return errors.New("actual database head drift")
	}
	rows, e := tx.Query(ctx, `SELECT p.oid::regprocedure::text,pg_get_functiondef(p.oid) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.prokind='f' ORDER BY p.oid::regprocedure::text COLLATE "C"`)
	if e != nil {
		return e
	}
	catalog := sha256.New()
	for rows.Next() {
		var name, body string
		if e = rows.Scan(&name, &body); e != nil {
			rows.Close()
			return e
		}
		fmt.Fprintf(catalog, "%d:%s%d:%s", len(name), name, len(body), body)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if hex.EncodeToString(catalog.Sum(nil)) != review.FunctionCatalogSHA {
		return errors.New("function catalog drift")
	}
	var workerEnabled, workerFresh bool
	var policy string
	var ops []byte
	if e = tx.QueryRow(ctx, `SELECT enabled,last_seen>clock_timestamp()-interval '30 seconds',capacity_policy_sha,qualified_operations FROM photo.worker_registrations WHERE worker_id='54c430d9-bcb6-40e9-8059-1e3eb284ce14'`).Scan(&workerEnabled, &workerFresh, &policy, &ops); e != nil {
		return e
	}
	var actual map[string][]string
	if json.Unmarshal(ops, &actual) != nil || !workerEnabled || !workerFresh || policy != review.Policy || len(actual) != 3 {
		return errors.New("current media worker authority unproven")
	}
	for _, op := range []string{"asset.verify", "media.thumbnail", "media.preview"} {
		v := actual[op]
		if len(v) != 1 || v[0] != "cpu" {
			return errors.New("current exact three operations required")
		}
	}
	n, e := one(ctx, tx, s, *component)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return errors.New("commit acknowledgement unknown; root reconcile, never automatic retry")
	}
	fmt.Printf("{\"status\":\"ONE_NORMAL_MEDIA_COMPONENT_COMMITTED_NOT_ACCEPTANCE\",\"component\":%q,\"count\":%d,\"operator_snapshot_context\":true,\"functions_role\":\"photo_scheduler\"}\n", *component, n)
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintf(os.Stderr, "MEDIA_COMPONENT_FAILURE_CLASS=%T\n", e)
		os.Exit(1)
	}
}
