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
	"photoeditor/backend/internal/workflow"
	"time"
)

type taskBinding struct {
	ID         uuid.UUID `json:"task_id"`
	Run        uuid.UUID `json:"run_id"`
	Class      string    `json:"resource_class"`
	Operation  string    `json:"operation"`
	ProfileSHA string    `json:"execution_profile_sha256"`
}
type scope struct {
	Tenant   uuid.UUID
	Tasks    []taskBinding
	Nodes    []string
	Jobs     []Job
	deferred map[string]deferBinding
	proof    *deferralReadback
}

func (s scope) ownsRun(id string) bool {
	for _, j := range s.Jobs {
		if j.Run.String() == id {
			return true
		}
	}
	return false
}
func validateJobs(jobs []Job) error {
	expected := map[uuid.UUID]string{uuid.MustParse("0b19bea8-4267-415b-892f-9764632541fb"): "ca70f3e72a0dec4af67fbc9a51ea24170979a92bc520c2040f6f320650f78568", uuid.MustParse("da99a0cd-6c91-4753-a26f-b3c41e425006"): "162cdd7851886967d8461a500c59ea806af1314a59216be41c9a21c351a7c1e2"}
	if len(jobs) != 2 {
		return errors.New("exact two jobs required")
	}
	seenRun := map[uuid.UUID]bool{}
	seenJob := map[uuid.UUID]bool{}
	seenPreset := map[uuid.UUID]bool{}
	for _, j := range jobs {
		if j.Tenant != uuid.MustParse("5dbe7f17-b560-4696-9525-03959e843841") || j.Run == uuid.Nil || j.Job == uuid.Nil || j.Selection == uuid.Nil || j.Selection != jobs[0].Selection || expected[j.Preset] == "" || expected[j.Preset] != j.PresetHash || seenRun[j.Run] || seenJob[j.Job] || seenPreset[j.Preset] {
			return errors.New("exact owner preset/job/selection scope drift")
		}
		seenRun[j.Run] = true
		seenJob[j.Job] = true
		seenPreset[j.Preset] = true
	}
	return nil
}
func (s scope) validate() error {
	if e := validateJobs(s.Jobs); e != nil {
		return e
	}
	if len(s.Tasks) != 6 || len(s.Jobs) != 2 || len(s.Nodes) != 3 {
		return errors.New("exact two-job six-task scope required")
	}
	nodes := map[string]bool{}
	for _, n := range s.Nodes {
		if n == "" || nodes[n] {
			return errors.New("exact distinct resource nodes required")
		}
		nodes[n] = true
	}
	seen := map[uuid.UUID]bool{}
	perRun := map[uuid.UUID]int{}
	for _, t := range s.Tasks {
		if t.ID == uuid.Nil || seen[t.ID] || !s.ownsRun(t.Run.String()) || t.Class != "cpu" || t.Operation != "export.encode" || len(t.ProfileSHA) != 64 {
			return errors.New("export task scope drift")
		}
		seen[t.ID] = true
		perRun[t.Run]++
	}
	if perRun[s.Jobs[0].Run] != 3 || perRun[s.Jobs[1].Run] != 3 {
		return errors.New("three tasks per preset required")
	}
	return nil
}
func observedTasks(ctx context.Context, tx pgx.Tx, s scope) error {
	var head int64
	if e := tx.QueryRow(ctx, "SELECT max(version_id)FROM public.goose_db_version WHERE is_applied").Scan(&head); e != nil || requireHead(head) != nil {
		return errors.New("head drift")
	}
	for _, j := range s.Jobs {
		var bound bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT FROM photo.download_jobs j JOIN photo.runs r ON r.tenant_id=j.tenant_id AND r.id=j.run_id WHERE j.tenant_id=$1 AND j.id=$2 AND j.run_id=$3 AND j.snapshot_id=$4 AND j.preset->>'id'=$5 AND j.preset->>'hash'=$6 AND j.principal_type='user' AND r.kind='download' AND r.input_snapshot_id=j.snapshot_id AND NOT r.cancel_requested AND NOT j.cancel_requested)", j.Tenant, j.Job, j.Run, j.Selection, j.Preset.String(), j.PresetHash).Scan(&bound); e != nil {
			return e
		}
		if !bound {
			return errors.New("actual download child binding drift")
		}
	}
	var globalActive, reserved, occupied, pending int
	if e := tx.QueryRow(ctx, "SELECT (SELECT count(*)FROM photo.tasks WHERE state IN('queued','running')),(SELECT count(*)FROM photo.resource_reservations WHERE state='reserved'),(SELECT count(*)FROM photo.resource_slots WHERE task_id IS NOT NULL),(SELECT count(*)FROM photo.outbox WHERE sent_at IS NULL)").Scan(&globalActive, &reserved, &occupied, &pending); e != nil {
		return e
	}
	if globalActive+reserved+occupied+pending != 0 {
		return errors.New("global component admission not quiescent")
	}
	runs := []uuid.UUID{s.Jobs[0].Run, s.Jobs[1].Run}
	var total, active, ready int
	if e := tx.QueryRow(ctx, "SELECT count(*),count(*)FILTER(WHERE state IN('queued','running')),count(*)FILTER(WHERE state='ready')FROM photo.tasks WHERE run_id=ANY($1)", runs).Scan(&total, &active, &ready); e != nil || total != 6 || active != 0 || ready < 1 {
		return errors.New("actual six-task quiescent inventory required")
	}
	for _, t := range s.Tasks {
		var tenant, run uuid.UUID
		var op, class, state string
		var raw []byte
		if e := tx.QueryRow(ctx, "SELECT tenant_id,run_id,operation,resource_class,execution_profile::text,state FROM photo.tasks WHERE id=$1", t.ID).Scan(&tenant, &run, &op, &class, &raw, &state); e != nil {
			return e
		}
		h := sha256.Sum256(raw)
		if state == "succeeded" {
			var current bool
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token WHERE t.id=$1 AND t.state='succeeded')", t.ID).Scan(&current); e != nil {
				return e
			}
			if !current {
				return errors.New("current canonical success required")
			}
		}
		if tenant != s.Tenant || run != t.Run || op != "export.encode" || class != "cpu" || hex.EncodeToString(h[:]) != t.ProfileSHA || (state != "ready" && state != "succeeded") {
			return errors.New("task/profile/state drift")
		}
	}
	return nil
}
func component(ctx context.Context, p *pgxpool.Pool, s scope) (int, error) {
	return componentWithBegin(ctx, p.Begin, func(tx pgx.Tx) (int, error) { return componentTX(ctx, tx, s) })
}
func componentWithBegin(ctx context.Context, begin func(context.Context) (pgx.Tx, error), body func(pgx.Tx) (int, error)) (int, error) {
	tx, e := begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	n, e := body(tx)
	if e != nil {
		return 0, e
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, e
	}
	return n, nil
}
func componentTX(ctx context.Context, tx pgx.Tx, s scope) (int, error) {
	if e := s.validate(); e != nil {
		return 0, e
	}
	if e := observedTasks(ctx, tx, s); e != nil {
		return 0, e
	}
	before, e := capture(ctx, tx, s)
	if e != nil {
		return 0, e
	}
	dbefore, e := s.deferRows(ctx, tx)
	if e != nil {
		return 0, e
	}
	started := time.Now().UTC()
	selected := map[string]bool{}
	if _, e = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); e != nil {
		return 0, e
	}
	count, e := dispatchBounded(s, selected, func(accept func(uuid.UUID, string, []byte) error) (bool, bool, error) {
		return (workflow.Dispatcher{}).DispatchOneInTransaction(ctx, tx, accept)
	})
	if e != nil {
		return 0, e
	}
	if _, e = tx.Exec(ctx, "RESET ROLE"); e != nil {
		return 0, e
	}
	dafter, e := s.deferRows(ctx, tx)
	if e != nil {
		return 0, e
	}
	ds := map[string]bool{}
	for id := range selected {
		if _, ok := s.deferred[id]; ok {
			ds[id] = true
		}
	}
	if e = validateDeferrals(dbefore, dafter, ds, started, time.Now().UTC()); e != nil {
		return 0, e
	}
	after, e := capture(ctx, tx, s)
	if e != nil {
		return 0, e
	}
	if !equalSnapshots(before, after) {
		return 0, errors.New("outside export dispatch effect refused")
	}
	return count, nil
}
func dispatch(ctx context.Context, input Scope) error {
	s := scope{Tenant: input.Jobs[0].Tenant, Tasks: input.Tasks, Nodes: input.Nodes, Jobs: input.Jobs}
	if e := s.validate(); e != nil {
		return e
	}
	if e := s.loadDeferrals(); e != nil {
		return e
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("DOWNLOAD_OBSERVER_DATABASE_URL"))
	if e != nil {
		return errors.New("explicit observer unavailable")
	}
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return e
	}
	defer pool.Close()
	var current, session string
	if e = pool.QueryRow(ctx, "SELECT current_user,session_user").Scan(&current, &session); e != nil || current != "rtx_snapshot_admin" || session != current {
		return errors.New("explicit observer principal required")
	}
	n, e := component(ctx, pool, s)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ONE_NORMAL_EXPORT_DISPATCH_NOT_OUTPUT_ACCEPTANCE", "count": n})
}

func validateSourceBatch(batch workflow.DownloadExportBatch, sources []SourceBinding) error {
	if len(sources) != 3 || len(batch.Spec.Items) != 3 {
		return errors.New("exact three processed masters required")
	}
	seen := map[uuid.UUID]bool{}
	for i, x := range sources {
		item := batch.Spec.Items[i]
		if x.Master == uuid.Nil || x.Pixels == uuid.Nil || seen[x.Master] || len(x.MasterSHA) != 64 || len(x.PixelsSHA) != 64 || x.Width < 1 || x.Height < 1 || item.Manifest.ID != x.Master || item.Manifest.SHA256 != x.MasterSHA || item.Image.Pixels.ID != x.Pixels || item.Image.Pixels.SHA256 != x.PixelsSHA || item.Image.Canvas.Width != x.Width || item.Image.Canvas.Height != x.Height {
			return errors.New("processed master/pixels/fullresolution binding drift")
		}
		seen[x.Master] = true
	}
	return nil
}
