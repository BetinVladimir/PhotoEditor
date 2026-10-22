// Source-only: one ordinary planning tick for an actual owner download job.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/workflow"
)

type Job struct {
	Tenant     uuid.UUID `json:"tenant_id"`
	Run        uuid.UUID `json:"run_id"`
	Job        uuid.UUID `json:"download_job_id"`
	Preset     uuid.UUID `json:"preset_id"`
	PresetHash string    `json:"preset_hash"`
	Selection  uuid.UUID `json:"selection_snapshot_id"`
}
type Scope struct {
	Schema   int       `json:"schema"`
	Head     int64     `json:"head"`
	Jobs     []Job     `json:"jobs"`
	Approved bool      `json:"approved_for_one_planning_tick"`
	Observed time.Time `json:"observed_at"`
}

// Claim is made in a caller transaction: an unexpected global candidate rolls
// back, rather than committing its lease and attempting a compensating release.
type scopedLeases struct {
	pool    *pgxpool.Pool
	allowed map[uuid.UUID]Job
}

func (s scopedLeases) Claim(ctx context.Context) (*workflow.DownloadBuildLease, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	l, e := (workflow.SQLDownloadBuildLeases{DB: tx}).Claim(ctx)
	if e != nil || l == nil {
		return l, e
	}
	j, ok := s.allowed[l.RunID]
	if !ok || j.Tenant != l.TenantID {
		return nil, errors.New("outside reviewed download scope")
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return l, nil
}
func (s scopedLeases) Renew(c context.Context, l workflow.DownloadBuildLease) (bool, error) {
	return (workflow.SQLDownloadBuildLeases{DB: s.pool}).Renew(c, l)
}
func (s scopedLeases) Release(c context.Context, l workflow.DownloadBuildLease) (bool, error) {
	return (workflow.SQLDownloadBuildLeases{DB: s.pool}).Release(c, l)
}

type planningRunner struct {
	phases  workflow.DownloadBuildPhaseSource
	exports workflow.DownloadExportCoordinator
}

func (r planningRunner) Build(c context.Context, t, run uuid.UUID) (bool, error) {
	p, e := r.phases.Load(c, t, run)
	if e != nil {
		return false, e
	}
	if p != "planning" {
		return false, errors.New("planning only; encoding/package advancement needs separate review")
	}
	return r.exports.Build(c, t, run)
}
func run() error {
	raw, e := os.ReadFile(os.Getenv("DOWNLOAD_SCOPE_FILE"))
	if e != nil {
		return errors.New("scope unavailable")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != os.Getenv("DOWNLOAD_SCOPE_SHA256") {
		return errors.New("scope pin mismatch")
	}
	var scope Scope
	if json.Unmarshal(raw, &scope) != nil || scope.Schema != 1 || scope.Head != 2654 || !scope.Approved || len(scope.Jobs) != 2 || time.Since(scope.Observed) < 0 || time.Since(scope.Observed) > 120*time.Second {
		return errors.New("fresh reviewed exact two-job scope required")
	}
	allowed := map[uuid.UUID]Job{}
	presets := map[uuid.UUID]bool{}
	for _, j := range scope.Jobs {
		if j.Tenant == uuid.Nil || j.Run == uuid.Nil || j.Job == uuid.Nil || j.Selection == uuid.Nil || j.Preset == uuid.Nil || len(j.PresetHash) != 64 || allowed[j.Run].Run != uuid.Nil || presets[j.Preset] {
			return errors.New("invalid closed scope")
		}
		allowed[j.Run] = j
		presets[j.Preset] = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(os.Getenv("SCHEDULER_DATABASE_URL"))
	if e != nil {
		return errors.New("database configuration unavailable")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	database.GuardApplicationRole(cfg)
	cfg.MaxConns = 3
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return e
	}
	defer pool.Close()

	icc := os.Getenv("ICC_DIRECTORY")
	work, e := colorprofile.LoadWorkSHA(icc)
	if e != nil {
		return e
	}
	targets, e := colorprofile.LoadSHAs(icc)
	if e != nil {
		return e
	}
	config := objectstore.ConfigFromEnv(true)
	objects, e := objectstore.New(config)
	if e != nil {
		return e
	}
	plans := workflow.DownloadExportPlanRepository{DB: pool}
	publisher := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: pool, Bucket: config.Bucket}, Objects: objects}
	coordinator := workflow.DownloadExportCoordinator{Source: workflow.DownloadExportLoader{DB: pool, Objects: objects, Bucket: config.Bucket, WorkICCSHA256: work, TargetICCs: targets}, Builder: workflow.DownloadExportBuilder{Plans: plans, Publisher: publisher}, Tasks: plans}
	worked, e := (workflow.DownloadBuildProcessor{Leases: scopedLeases{pool, allowed}, Runner: planningRunner{workflow.DownloadBuildPhaseSource{DB: pool}, coordinator}, WorkTimeout: 90 * time.Second}).ProcessOne(ctx)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ONE_NORMAL_DOWNLOAD_PLANNING_TICK_NOT_ENCODE_OR_DOWNLOAD_ACCEPTANCE", "worked": worked})
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintf(os.Stderr, "DOWNLOAD_COMPONENT_FAILURE_CLASS=%T\n", e)
		os.Exit(1)
	}
}
