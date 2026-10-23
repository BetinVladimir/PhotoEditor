package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/workflow"
	"strconv"
	"time"
)

var errAdvancePhase = errors.New("download phase differs from explicit approved phase")

const sourceHistoryPath = "/Users/freelancer/Documents/PhotoEditor/work/user-three-export-planning-host-r4/attempt-1792710488860346000/0002-scope.private.json"
const sourceHistorySHA = "e46e065914016c6c694b3e92e553309c514df63f38916f73b77633b561c6f038"

func advancePhase(p string) bool { return p == "encoding" || p == "packaging" || p == "archiving" }
func validateAdvanceScope(s Scope) error {
	if e := validateScopeEnvelope(s, "advance", time.Now()); e != nil {
		return e
	}
	if s.Component != "advance" || !advancePhase(s.ExpectedPhase) || s.AdvanceRun == uuid.Nil || s.Main2655InstallReceiptSHA != "275a0cd4db590d8d37bea9c9d6cab10794438baa60f5076322285e62fca993f5" {
		return errors.New("explicit MAIN2655 phase scope required")
	}
	if e := validateJobs(s.Jobs); e != nil {
		return e
	}
	expected := map[uuid.UUID]uuid.UUID{uuid.MustParse("d8108e3d-9609-4f74-b84a-b1b708bba11b"): uuid.MustParse("6937cd52-fcfe-4ed9-b94a-ea6991cf8c7f"), uuid.MustParse("e991510e-6de9-4aa3-ad25-2e9192a49f6d"): uuid.MustParse("3768a248-b8d7-48da-ab3f-2c57079ed644")}
	found := false
	for _, j := range s.Jobs {
		if expected[j.Run] != j.Job || j.Selection != uuid.MustParse("b815b9d5-50dd-48fb-ba08-1e3dbbfe1606") {
			return errors.New("actual two owner jobs required")
		}
		if j.Run == s.AdvanceRun {
			found = true
		}
	}
	if !found || s.VolumeByteLimit < 1 || s.VolumeByteLimit > workflow.DefaultVolumeByteLimit || s.VolumeFileLimit < 1 || s.VolumeFileLimit > workflow.MaxDownloadItems {
		return errors.New("actual phase target and normal volume limits required")
	}
	raw, e := os.ReadFile(sourceHistoryPath)
	if e != nil {
		return errors.New("actual processed source history unavailable")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != sourceHistorySHA {
		return errors.New("actual processed source history drift")
	}
	var saved Scope
	if json.Unmarshal(raw, &saved) != nil || len(saved.Sources) != 3 || len(s.Sources) != 3 {
		return errors.New("actual three processed sources required")
	}
	for i, x := range saved.Sources {
		if s.Sources[i] != x {
			return errors.New("processed source ordering or ref drift")
		}
	}
	return nil
}

type phaseErrorRow struct{ err error }

func (r phaseErrorRow) Scan(...any) error { return r.err }

type guardedPhaseDB struct {
	DB       artifactjson.RowQuerier
	Job      Job
	Expected string
	Before   func(context.Context) error
}
type guardedPhaseRow struct {
	raw      pgx.Row
	expected string
	ctx      context.Context
	before   func(context.Context) error
}

func (r guardedPhaseRow) Scan(dst ...any) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if len(dst) != 1 {
		return errors.New("closed phase row shape")
	}
	value, ok := dst[0].(*string)
	if !ok {
		return errors.New("closed phase destination")
	}
	var phase string
	if err := r.raw.Scan(&phase); err != nil {
		return err
	}
	if phase != r.expected {
		return errAdvancePhase
	}
	if r.before == nil {
		return errors.New("phase source binding guard missing")
	}
	if err := r.before(r.ctx); err != nil {
		return err
	}
	if err := r.ctx.Err(); err != nil {
		return err
	}
	*value = phase
	return nil
}
func (g guardedPhaseDB) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if g.DB == nil || !advancePhase(g.Expected) || q != "SELECT photo.download_build_phase($1,$2)" || len(args) != 2 || args[0] != g.Job.Tenant || args[1] != g.Job.Run {
		return phaseErrorRow{errors.New("closed phase query authority")}
	}
	return guardedPhaseRow{raw: g.DB.QueryRow(ctx, q, args...), expected: g.Expected, ctx: ctx, before: g.Before}
}

// Observer is genuine read-only authority. It never executes a mutating function
// or replaces photo_scheduler for the ordinary production phase methods.
func advanceBindings(ctx context.Context, observer *pgxpool.Pool, s Scope, j Job) error {
	tx, e := observer.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var head int64
	if e = tx.QueryRow(ctx, "SELECT max(version_id)FROM public.goose_db_version WHERE is_applied").Scan(&head); e != nil {
		return e
	}
	if e = requireHead(head); e != nil {
		return e
	}
	var ok bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.download_jobs j JOIN photo.runs r ON r.tenant_id=j.tenant_id AND r.id=j.run_id JOIN photo.snapshots snap ON snap.tenant_id=r.tenant_id AND snap.id=r.input_snapshot_id JOIN photo.download_build_state b ON b.tenant_id=r.tenant_id AND b.run_id=r.id WHERE j.tenant_id=$1 AND j.id=$2 AND j.run_id=$3 AND j.snapshot_id=$4 AND r.input_snapshot_id=$4 AND j.preset->>'id'=$5 AND j.preset->>'hash'=$6 AND j.principal_type='user' AND r.kind='download' AND r.status='running' AND j.status='building' AND NOT r.cancel_requested AND NOT j.cancel_requested AND b.phase=$7 AND b.item_count=3 AND snap.kind='selection' AND snap.item_count=3 AND(snap.expires_at IS NULL OR snap.expires_at>clock_timestamp()))`, j.Tenant, j.Job, j.Run, j.Selection, j.Preset.String(), j.PresetHash, s.ExpectedPhase).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("actual job selection preset phase binding drift")
	}
	rows, e := tx.Query(ctx, `SELECT p.ordinal,p.inputs,t.execution_profile->'resource_estimate'->>'pixel_count',si.artifact_id::text,si.sha256,m.state='ready' AND m.verified_at IS NOT NULL AND m.object_version IS NOT NULL AND m.object_version NOT IN('','null') AND(m.expires_at IS NULL OR m.expires_at>clock_timestamp()) AND x.state='ready' AND x.verified_at IS NOT NULL AND x.object_version IS NOT NULL AND x.object_version NOT IN('','null') AND(x.expires_at IS NULL OR x.expires_at>clock_timestamp()) FROM photo.download_export_plans p JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id AND t.run_id=p.run_id AND t.operation='export.encode' JOIN photo.snapshot_items si ON si.tenant_id=p.tenant_id AND si.snapshot_id=$3 AND si.ordinal=p.ordinal JOIN photo.artifacts m ON m.tenant_id=p.tenant_id AND m.id=(p.inputs->0->>'artifact_id')::uuid AND m.sha256=p.inputs->0->>'sha256' JOIN photo.artifacts x ON x.tenant_id=p.tenant_id AND x.id=(p.inputs->1->>'artifact_id')::uuid AND x.sha256=p.inputs->1->>'sha256' WHERE p.tenant_id=$1 AND p.run_id=$2 ORDER BY p.ordinal`, j.Tenant, j.Run, j.Selection)
	if e != nil {
		return e
	}
	count := 0
	for rows.Next() {
		var ordinal int
		var inputs []byte
		var pixel string
		var master, hash string
		var live bool
		if e = rows.Scan(&ordinal, &inputs, &pixel, &master, &hash, &live); e != nil {
			rows.Close()
			return e
		}
		if ordinal != count || count >= len(s.Sources) || !live {
			rows.Close()
			return errors.New("actual three source rows required")
		}
		x := s.Sources[count]
		var refs []struct {
			Artifact uuid.UUID `json:"artifact_id"`
			SHA      string    `json:"sha256"`
		}
		if json.Unmarshal(inputs, &refs) != nil || len(refs) != 2 || refs[0].Artifact != x.Master || refs[0].SHA != x.MasterSHA || refs[1].Artifact != x.Pixels || refs[1].SHA != x.PixelsSHA || master != x.Master.String() || hash != x.MasterSHA || pixel != formatPixels(x.Width*x.Height) {
			rows.Close()
			return errors.New("canonical source master pixels dimensions drift")
		}
		count++
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if count != 3 {
		return errors.New("missing processed source plan")
	}
	return nil
}

func formatPixels(pixels int64) string { return strconv.FormatInt(pixels, 10) }

func resolveAdvanceLimits(s Scope, getenv func(string) string) (workflow.DownloadVolumeLimits, error) {
	limits, e := workflow.DownloadVolumeLimitsFromEnvironment(getenv)
	if e != nil {
		return limits, e
	}
	if limits.Bytes != s.VolumeByteLimit || limits.Files != s.VolumeFileLimit {
		return limits, errors.New("configured normal volume limits differ from approved scope")
	}
	return limits, nil
}

func runAdvance(ctx context.Context, pool *pgxpool.Pool, s Scope, exports workflow.DownloadExportCoordinator, publisher artifactjson.Publisher) error {
	if e := validateAdvanceScope(s); e != nil {
		return e
	}
	limits, e := resolveAdvanceLimits(s, os.Getenv)
	if e != nil {
		return e
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("DOWNLOAD_OBSERVER_DATABASE_URL"))
	if e != nil {
		return errors.New("explicit readonly observer required")
	}
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	observer, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return e
	}
	defer observer.Close()
	var current, session string
	if e = observer.QueryRow(ctx, "SELECT current_user,session_user").Scan(&current, &session); e != nil || current != "rtx_snapshot_admin" || session != current {
		return errors.New("genuine readonly observer principal required")
	}
	var job Job
	for _, j := range s.Jobs {
		if j.Run == s.AdvanceRun {
			job = j
		}
	}
	guard := func(c context.Context) error { return advanceBindings(c, observer, s, job) }
	phases := workflow.DownloadBuildPhaseSource{DB: guardedPhaseDB{DB: pool, Job: job, Expected: s.ExpectedPhase, Before: guard}}
	runner := workflow.DownloadBuildRunnerV2{Phases: phases, Exports: exports,
		Packaging: workflow.DownloadPackagingCoordinator{ByteLimit: limits.Bytes, FileLimit: limits.Files, Outcomes: workflow.DownloadEncodeOutcomeSource{DB: pool}, Plans: workflow.DownloadPackagingRepository{DB: pool}},
		Archives:  workflow.DownloadArchiveCoordinator{Source: workflow.DownloadArchiveContextSource{DB: pool}, Builder: workflow.DownloadArchiveBuilder{Plans: workflow.DownloadArchiveRepository{DB: pool}, Publisher: publisher}, Tasks: workflow.DownloadArchiveTaskRepository{DB: pool}},
		Finalize:  workflow.DownloadArchiveFinalizer{DB: pool}}
	leases := scopedLeases{pool: pool, allowed: map[uuid.UUID]Job{job.Run: job}, guard: func(c context.Context, tx pgx.Tx, l *workflow.DownloadBuildLease) error {
		_, err := (workflow.DownloadBuildPhaseSource{DB: guardedPhaseDB{DB: tx, Job: job, Expected: s.ExpectedPhase, Before: guard}}).Load(c, l.TenantID, l.RunID)
		return err
	}}
	worked, e := (workflow.DownloadBuildProcessor{Leases: leases, Runner: runner, WorkTimeout: 90 * time.Second}).ProcessOne(ctx)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ONE_NORMAL_DOWNLOAD_PHASE_RETURNED_REQUIRES_ROOT_READBACK", "expected_phase": s.ExpectedPhase, "target_run_id": s.AdvanceRun, "worked": worked})
}
