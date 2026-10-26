// Source-only: one ordinary planning tick for an actual owner download job.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
type SourceBinding struct {
	Master    uuid.UUID `json:"master_artifact_id"`
	MasterSHA string    `json:"master_sha256"`
	Pixels    uuid.UUID `json:"pixels_artifact_id"`
	PixelsSHA string    `json:"pixels_sha256"`
	Width     int64     `json:"width"`
	Height    int64     `json:"height"`
}
type Scope struct {
	RecoveryPhase             string          `json:"recovery_phase"`
	ExpectedPhase             string          `json:"expected_phase"`
	AdvanceRun                uuid.UUID       `json:"advance_run_id"`
	VolumeByteLimit           int64           `json:"volume_byte_limit"`
	VolumeFileLimit           int             `json:"volume_file_limit"`
	Main2658InstallReceiptSHA string          `json:"main_2658_install_receipt_sha256"`
	Sources                   []SourceBinding `json:"processed_sources"`
	Component                 string          `json:"component"`
	Tasks                     []taskBinding   `json:"tasks"`
	Nodes                     []string        `json:"resource_node_ids"`
	Schema                    int             `json:"schema"`
	Head                      int64           `json:"head"`
	Jobs                      []Job           `json:"jobs"`
	Approved                  bool            `json:"approved_for_one_download_component"`
	Observed                  time.Time       `json:"observed_at"`
}

// Claim is made in a caller transaction: an unexpected global candidate rolls
// back, rather than committing its lease and attempting a compensating release.
type scopedLeases struct {
	begin   func(context.Context) (pgx.Tx, error)
	pool    *pgxpool.Pool
	allowed map[uuid.UUID]Job
	guard   func(context.Context, pgx.Tx, *workflow.DownloadBuildLease) error
}

func (s scopedLeases) Claim(ctx context.Context) (*workflow.DownloadBuildLease, error) {
	begin := s.begin
	if begin == nil {
		begin = s.pool.Begin
	}
	tx, e := begin(ctx)
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
	if s.guard != nil {
		if e = s.guard(ctx, tx, l); e != nil {
			return nil, e
		}
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
	jobs    map[uuid.UUID]Job
	sources []SourceBinding
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
	j, ok := r.jobs[run]
	if !ok || j.Tenant != t {
		return false, errors.New("outside exact planning job")
	}
	if e = validatePersistedPreset(c, r.exports.Source.DB, j); e != nil {
		return false, e
	}
	batch, e := r.exports.Source.Load(c, t, run)
	if e != nil {
		return false, e
	}
	return approvedBatchThenPublish(batch, j, r.sources, func() (bool, error) { return r.exports.Build(c, t, run) })
}

const requiredHead int64 = 2658

// Empty until configure.py binds a real independently reviewed installed receipt.
const requiredInstallReceiptSHA = "881bb7228c41a6f513079a4847db96173a429c58a4d0cbe238a734a8b1586d19"

func requireHead(head int64) error {
	if head != requiredHead {
		return errors.New("head drift")
	}
	return nil
}
func validateScopeEnvelope(scope Scope, component string, now time.Time) error {
	if requireHead(scope.Head) != nil || scope.Schema != 1 || scope.Component != component || !scope.Approved || len(scope.Jobs) != 2 || now.Sub(scope.Observed) < 0 || now.Sub(scope.Observed) > 120*time.Second {
		return errors.New("fresh reviewed exact two-job scope required")
	}
	if len(requiredInstallReceiptSHA) != 64 || scope.Main2658InstallReceiptSHA != requiredInstallReceiptSHA || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(scope.Main2658InstallReceiptSHA) || scope.Main2658InstallReceiptSHA == "0000000000000000000000000000000000000000000000000000000000000000" {
		return errors.New("actual MAIN2658 installation receipt binding required")
	}
	return nil
}

func run() error {
	component := os.Getenv("DOWNLOAD_COMPONENT")
	if component != "dispatch" && component != "advance" && component != "recovery" {
		return errors.New("explicit closed component required")
	}
	raw, e := os.ReadFile(os.Getenv("DOWNLOAD_SCOPE_FILE"))
	if e != nil {
		return errors.New("scope unavailable")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != os.Getenv("DOWNLOAD_SCOPE_SHA256") {
		return errors.New("scope pin mismatch")
	}
	var scope Scope
	if json.Unmarshal(raw, &scope) != nil {
		return errors.New("fresh reviewed exact two-job scope required")
	}
	if e = validateScopeEnvelope(scope, component, time.Now()); e != nil {
		return e
	}
	if component == "advance" {
		if e = validateAdvanceScope(scope); e != nil {
			return e
		}
	}
	if e = validateJobs(scope.Jobs); e != nil {
		return e
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
	if component == "dispatch" {
		return dispatch(ctx, scope)
	}
	if component == "recovery" {
		return recoverArchives(ctx, scope)
	}
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
	if component == "advance" {
		return runAdvance(ctx, pool, scope, coordinator, publisher)
	}
	worked, e := (workflow.DownloadBuildProcessor{Leases: scopedLeases{pool: pool, allowed: allowed}, Runner: planningRunner{jobs: allowed, sources: scope.Sources, phases: workflow.DownloadBuildPhaseSource{DB: pool}, exports: coordinator}, WorkTimeout: 90 * time.Second}).ProcessOne(ctx)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ONE_NORMAL_DOWNLOAD_PLANNING_TICK_NOT_ENCODE_OR_DOWNLOAD_ACCEPTANCE", "worked": worked})
}
func main() {
	if e := run(); e != nil {
		safe := map[string]string{"error_class": fmt.Sprintf("%T", e)}
		var pg *pgconn.PgError
		if errors.As(e, &pg) {
			if regexp.MustCompile(`^[A-Z0-9]{5}$`).MatchString(pg.Code) {
				safe["sqlstate"] = pg.Code
			}
			if regexp.MustCompile(`^[A-Za-z0-9_]{1,96}$`).MatchString(pg.Routine) {
				safe["routine"] = pg.Routine
			}
		}
		_ = json.NewEncoder(os.Stderr).Encode(safe)
		os.Exit(1)
	}
}
