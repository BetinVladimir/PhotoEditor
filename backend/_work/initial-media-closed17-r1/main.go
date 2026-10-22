// Explicit operator diagnostic: normal media coordinator, no production SQL changes.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	mediascope "photoeditor/backend/_work/media-retry-scope-r1"
	retryscope "photoeditor/backend/_work/user-raw-retry-scope-r3"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/workflow"
	"time"
)

const scopePath = "/Users/freelancer/Documents/PhotoEditor/work/rtx-initial-media-audit-r1/closed-scope.json"
const scopeSHA = "a7cc480d429ce9d077eb180355ae2277402d7808732496e6a90d073e4166b547"

func loadScope(raw []byte, pin string) (mediascope.Scope, error) {
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != pin {
		return nil, errors.New("closed media scope digest drift")
	}
	var v struct {
		Schema   int                  `json:"schema_version"`
		Bindings []mediascope.Binding `json:"bindings"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Schema != 1 {
		return nil, errors.New("invalid closed media scope")
	}
	return mediascope.NewScope(v.Bindings)
}
func reviewed(path, pin string) error {
	raw, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	if len(pin) != 64 || hex.EncodeToString(sum[:]) != pin {
		return errors.New("root review digest required")
	}
	var v struct {
		Approved       bool   `json:"approved"`
		Scope          string `json:"scope_sha256"`
		WorkerEvidence string `json:"physical_media_worker_evidence_sha256"`
		Quiescence     string `json:"current_quiescence_receipt_sha256"`
	}
	if json.Unmarshal(raw, &v) != nil || !v.Approved || v.Scope != scopeSHA || len(v.WorkerEvidence) != 64 || len(v.Quiescence) != 64 {
		return errors.New("root media worker and scope review required")
	}
	return nil
}
func run() error {
	compose := flag.Bool("compose", false, "explicit normal closed17 media composition")
	review := flag.String("review", "", "root independent review file")
	reviewSHA := flag.String("review-sha256", "", "exact root review SHA")
	limit := flag.Int("limit", 17, "maximum normal job claims, 1..17")
	flag.Parse()
	if !*compose || *limit < 1 || *limit > 17 {
		return errors.New("explicit bounded composition and root review required")
	}
	if e := reviewed(*review, *reviewSHA); e != nil {
		return e
	}
	raw, e := os.ReadFile(scopePath)
	if e != nil {
		return e
	}
	scope, e := loadScope(raw, scopeSHA)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if e != nil {
		return e
	}
	cfg.MaxConns = 3
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	database.GuardApplicationRole(cfg)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return e
	}
	defer pool.Close()
	if e = database.RequireApplicationRole(ctx, pool, "photo_scheduler"); e != nil {
		return e
	}
	config := objectstore.ConfigFromEnv(true)
	objects, e := objectstore.New(config)
	if e != nil {
		return e
	}
	profiles, e := colorprofile.LoadSHAs(os.Getenv("ICC_DIRECTORY"))
	if e != nil {
		return e
	}
	store := workflow.MediaRetryStore{DB: pool, Objects: objects, Bucket: config.Bucket}
	guarded := mediascope.Store{MediaRetryStore: store, Scope: scope, BeginClaimTx: func(c context.Context) (retryscope.Transaction, error) { return pool.Begin(c) }}
	processor := workflow.MediaRetryCoordinator{Store: guarded, Publisher: artifactjson.Publisher{Repository: workflow.MediaRetryDocuments{DB: pool, Bucket: config.Bucket}, Objects: objects}, SRGBSHA: profiles["srgb_v1"]}
	count := 0
	for i := 0; i < *limit; i++ {
		changed, e := processor.ProcessOne(ctx)
		if e != nil {
			return e
		}
		if !changed {
			break
		}
		count++
	}
	// No global dispatcher/finalizer is called here: root must separately validate
	// no outside work, scoped derived runs/tasks, physical worker and own queues.
	fmt.Printf("{\"status\":\"BOUNDED_NORMAL_MEDIA_COMPOSITION_FINISHED_NOT_EXECUTION_ACCEPTANCE\",\"composed\":%d,\"max_claims\":%d,\"dispatch_or_finalize\":false}\n", count, *limit)
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintf(os.Stderr, "MEDIA_COMPOSITION_FAILURE_CLASS=%T\n", e)
		os.Exit(1)
	}
}
