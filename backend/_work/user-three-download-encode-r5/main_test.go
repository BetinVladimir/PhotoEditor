package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/workflow"
	"reflect"
	"strings"
	"testing"
	"time"
)

type leaseRow struct {
	values []any
	err    error
}

func (r leaseRow) Scan(dst ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, v := range r.values {
		reflect.ValueOf(dst[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

type leaseTx struct {
	pgx.Tx
	row                leaseRow
	commits, rollbacks int
	commitErr          error
}

func (t *leaseTx) QueryRow(context.Context, string, ...any) pgx.Row { return t.row }
func (t *leaseTx) Commit(context.Context) error                     { t.commits++; return t.commitErr }
func (t *leaseTx) Rollback(context.Context) error                   { t.rollbacks++; return nil }
func TestCheckedClaimCommitOrRollback(t *testing.T) {
	tenant, run, token := uuid.New(), uuid.New(), uuid.New()
	allowed := map[uuid.UUID]Job{run: {Tenant: tenant, Run: run}}
	for _, tc := range []struct {
		name       string
		row        leaseRow
		allowed    map[uuid.UUID]Job
		commitErr  error
		wantCommit int
		wantErr    bool
	}{
		{"exact", leaseRow{values: []any{tenant, run, token, time.Now().Add(time.Minute)}}, allowed, nil, 1, false},
		{"foreignrun", leaseRow{values: []any{tenant, uuid.New(), token, time.Now()}}, allowed, nil, 0, true},
		{"foreigntenant", leaseRow{values: []any{uuid.New(), run, token, time.Now()}}, allowed, nil, 0, true},
		{"empty", leaseRow{err: pgx.ErrNoRows}, allowed, nil, 0, false},
		{"claimfailure", leaseRow{err: errors.New("typed failure")}, allowed, nil, 0, true},
		{"unknowncommit", leaseRow{values: []any{tenant, run, token, time.Now()}}, allowed, errors.New("unknowncommit"), 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &leaseTx{row: tc.row, commitErr: tc.commitErr}
			s := scopedLeases{allowed: tc.allowed, begin: func(context.Context) (pgx.Tx, error) { return tx, nil }}
			_, e := s.Claim(context.Background())
			if (e != nil) != tc.wantErr || tx.commits != tc.wantCommit || tx.rollbacks != 1 {
				t.Fatalf("wrong transaction outcome commit=%d rollback=%d err=%v", tx.commits, tx.rollbacks, e)
			}
		})
	}
}
func dispatchScope() scope {
	tenant := uuid.New()
	jobs := []Job{{Tenant: tenant, Run: uuid.New()}, {Tenant: tenant, Run: uuid.New()}}
	s := scope{Tenant: tenant, Jobs: jobs, Nodes: []string{"a", "b", "c"}, deferred: map[string]deferBinding{}}
	for i := 0; i < 6; i++ {
		s.Tasks = append(s.Tasks, taskBinding{ID: uuid.New(), Run: jobs[i/3].Run, Class: "cpu", Operation: "export.encode", ProfileSHA: strings.Repeat("a", 64)})
	}
	return s
}
func TestOneOwnSelectionStopsEvenWaitingResource(t *testing.T) {
	s := dispatchScope()
	calls := 0
	selected := map[string]bool{}
	n, e := dispatchBounded(s, selected, func(accept func(uuid.UUID, string, []byte) error) (bool, bool, error) {
		calls++
		return true, false, accept(s.Tasks[0].ID, "cpu", []byte("profile"))
	})
	if e == nil || n != 0 || calls != 1 {
		t.Fatal("wrong profile must refuse before nextselection")
	}
}
func TestNormalSixDeferralsThenOneOwn(t *testing.T) {
	s := dispatchScope()
	raw := []byte("{}")
	s.Tasks[0].ProfileSHA = profileHash(raw)
	ids := []uuid.UUID{}
	for i := 0; i < 6; i++ {
		id := uuid.New()
		ids = append(ids, id)
		s.deferred[id.String()] = deferBinding{Task: id.String()}
	}
	ids = append(ids, s.Tasks[0].ID)
	calls := 0
	n, e := dispatchBounded(s, map[string]bool{}, func(accept func(uuid.UUID, string, []byte) error) (bool, bool, error) {
		id := ids[calls]
		class := "media"
		if calls == 6 {
			class = "cpu"
		}
		calls++
		return true, true, accept(id, class, raw)
	})
	if e != nil || n != 7 || calls != 7 {
		t.Fatalf("six+one failed %d %d %v", n, calls, e)
	}
}
func TestOwnWaitingResourceDoesNotSelectSecond(t *testing.T) {
	s := dispatchScope()
	raw := []byte("{}")
	s.Tasks[0].ProfileSHA = profileHash(raw)
	calls := 0
	_, e := dispatchBounded(s, map[string]bool{}, func(accept func(uuid.UUID, string, []byte) error) (bool, bool, error) {
		calls++
		return true, true, accept(s.Tasks[0].ID, "cpu", raw)
	})
	if e != nil || calls != 1 {
		t.Fatal("selected second own")
	}
}

func profileHash(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func TestOutsideRowsUnknownFieldsProtected(t *testing.T) {
	s := dispatchScope()
	for _, table := range []string{"artifacts", "download_jobs", "download_export_plans", "runs", "asset_versions"} {
		skip, e := s.normalize(table, map[string]json.RawMessage{"tenant_id": json.RawMessage(`"` + s.Tenant.String() + `"`), "run_id": json.RawMessage(`"` + s.Jobs[0].Run.String() + `"`), "unknown_future_authority": json.RawMessage(`true`)})
		if skip || e != nil {
			t.Fatal("blanket outside row exclusion", table, e)
		}
	}
}

func TestDispatchCallerTransactionOutcome(t *testing.T) {
	for _, bad := range []bool{false, true} {
		tx := &leaseTx{}
		n, e := componentWithBegin(context.Background(), func(context.Context) (pgx.Tx, error) { return tx, nil }, func(pgx.Tx) (int, error) {
			if bad {
				return 0, errors.New("foreign candidate or outside effect")
			}
			return 1, nil
		})
		if bad {
			if e == nil || tx.commits != 0 {
				t.Fatal("failed caller transaction committed")
			}
		} else if e != nil || n != 1 || tx.commits != 1 {
			t.Fatal("reviewed component not committed")
		}
		if tx.rollbacks != 1 {
			t.Fatal("caller cleanup missing")
		}
	}
}

func validJobs() []Job {
	tenant := uuid.MustParse("5dbe7f17-b560-4696-9525-03959e843841")
	selection := uuid.New()
	return []Job{{Tenant: tenant, Run: uuid.New(), Job: uuid.New(), Selection: selection, Preset: uuid.MustParse("0b19bea8-4267-415b-892f-9764632541fb"), PresetHash: "ca70f3e72a0dec4af67fbc9a51ea24170979a92bc520c2040f6f320650f78568"}, {Tenant: tenant, Run: uuid.New(), Job: uuid.New(), Selection: selection, Preset: uuid.MustParse("da99a0cd-6c91-4753-a26f-b3c41e425006"), PresetHash: "162cdd7851886967d8461a500c59ea806af1314a59216be41c9a21c351a7c1e2"}}
}
func TestExactOwnerTwoPresetScope(t *testing.T) {
	if e := validateJobs(validJobs()); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func([]Job){func(j []Job) { j[1].Tenant = uuid.New() }, func(j []Job) { j[1].Selection = uuid.New() }, func(j []Job) { j[1].PresetHash = strings.Repeat("f", 64) }, func(j []Job) { j[1].Run = j[0].Run }, func(j []Job) { j[1].Preset = j[0].Preset }} {
		j := validJobs()
		mutate(j)
		if validateJobs(j) == nil {
			t.Fatal("closed scope substitution accepted")
		}
	}
}

func TestExactThreeProcessedSourcesBeforePublication(t *testing.T) {
	var batch workflow.DownloadExportBatch
	var sources []SourceBinding
	for i := 0; i < 3; i++ {
		x := SourceBinding{Master: uuid.New(), MasterSHA: strings.Repeat("a", 64), Pixels: uuid.New(), PixelsSHA: strings.Repeat("b", 64), Width: 4180, Height: 6264}
		sources = append(sources, x)
		batch.Spec.Items = append(batch.Spec.Items, workflow.DownloadExportItem{Ordinal: int64(i), Manifest: review.ArtifactRef{ID: x.Master, SHA256: x.MasterSHA}, Image: review.WorkingImage{Pixels: review.ArtifactRef{ID: x.Pixels, SHA256: x.PixelsSHA}, Canvas: review.Canvas{Width: x.Width, Height: x.Height}}})
	}
	if e := validateSourceBatch(batch, sources); e != nil {
		t.Fatal(e)
	}
	sources[0].Width = 2560
	if validateSourceBatch(batch, sources) == nil {
		t.Fatal("preview-sized substituted source admitted")
	}
	sources[0].Width = 4180
	sources[1].Master = uuid.New()
	if validateSourceBatch(batch, sources) == nil {
		t.Fatal("foreign processed master admitted")
	}
}
