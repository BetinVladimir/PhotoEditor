package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/workflow"
	"testing"
	"time"
)

type phaseFixtureDB struct {
	phase string
	calls int
}

func (d *phaseFixtureDB) QueryRow(context.Context, string, ...any) pgx.Row {
	d.calls++
	return leaseRow{values: []any{d.phase}}
}
func actualAdvanceFixture(t *testing.T) Scope {
	t.Helper()
	raw, e := os.ReadFile(sourceHistoryPath)
	if e != nil {
		t.Fatal(e)
	}
	var s Scope
	if e = json.Unmarshal(raw, &s); e != nil {
		t.Fatal(e)
	}
	s.Component = "advance"
	s.ExpectedPhase = "encoding"
	s.AdvanceRun = uuid.MustParse("d8108e3d-9609-4f74-b84a-b1b708bba11b")
	s.VolumeByteLimit = workflow.DefaultVolumeByteLimit
	s.VolumeFileLimit = workflow.MaxDownloadItems
	s.Observed = time.Now()
	return s
}
func TestAdvanceScopeExactActualSourcesTargetPhase(t *testing.T) {
	s := actualAdvanceFixture(t)
	if e := validateAdvanceScope(s); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"phase", "target", "source", "order", "dimensions", "job", "head", "receipt", "limit"} {
		t.Run(kind, func(t *testing.T) {
			v := s
			v.Sources = append([]SourceBinding(nil), s.Sources...)
			v.Jobs = append([]Job(nil), s.Jobs...)
			switch kind {
			case "phase":
				v.ExpectedPhase = "planning"
			case "target":
				v.AdvanceRun = uuid.New()
			case "source":
				v.Sources[0].Master = uuid.New()
			case "order":
				v.Sources[0], v.Sources[1] = v.Sources[1], v.Sources[0]
			case "dimensions":
				v.Sources[0].Width, v.Sources[0].Height = v.Sources[0].Height, v.Sources[0].Width
			case "job":
				v.Jobs[0].Job = uuid.New()
			case "head":
				v.Head = 2654
			case "receipt":
				v.Main2655InstallReceiptSHA = ""
			case "limit":
				v.VolumeFileLimit = 0
			}
			if validateAdvanceScope(v) == nil {
				t.Fatal("closed source/phase/target scope widened")
			}
		})
	}
}
func TestPhaseGateUsesActualProductionRunnerBeforeAnyBranch(t *testing.T) {
	job := actualAdvanceFixture(t).Jobs[0]
	db := &phaseFixtureDB{phase: "packaging"}
	guardCalls := 0
	gate := guardedPhaseDB{DB: db, Job: job, Expected: "encoding", Before: func(context.Context) error { guardCalls++; return nil }}
	// This is the genuine production switch. Wrong actual phase must fail at its
	// own PhaseSource.Load, before entering an unconfigured publishing coordinator.
	runner := workflow.DownloadBuildRunnerV2{Phases: workflow.DownloadBuildPhaseSource{DB: gate}}
	_, e := runner.Build(context.Background(), job.Tenant, job.Run)
	if !errors.Is(e, errAdvancePhase) || guardCalls != 0 || db.calls != 1 {
		t.Fatal("production runner bypassed exact phase guard")
	}
}
func TestPhaseGateCorrectPhaseAndGuardFailure(t *testing.T) {
	job := actualAdvanceFixture(t).Jobs[0]
	sentinel := errors.New("readonly source guard refused")
	for _, refuse := range []bool{false, true} {
		db := &phaseFixtureDB{phase: "packaging"}
		called := 0
		gate := guardedPhaseDB{DB: db, Job: job, Expected: "packaging", Before: func(context.Context) error {
			called++
			if refuse {
				return sentinel
			}
			return nil
		}}
		phase, e := (workflow.DownloadBuildPhaseSource{DB: gate}).Load(context.Background(), job.Tenant, job.Run)
		if called != 1 || db.calls != 1 {
			t.Fatal("normal phase source not measured exactly once")
		}
		if refuse {
			if !errors.Is(e, sentinel) || phase != "" {
				t.Fatal("source guard error lost")
			}
		} else if e != nil || phase != "packaging" {
			t.Fatal("correct phase refused")
		}
	}
}
func TestPhaseGateRejectsForeignOrUnknownQueryBeforeSQL(t *testing.T) {
	job := actualAdvanceFixture(t).Jobs[0]
	for _, kind := range []string{"tenant", "run", "query", "phase"} {
		db := &phaseFixtureDB{phase: "encoding"}
		gate := guardedPhaseDB{DB: db, Job: job, Expected: "encoding", Before: func(context.Context) error { return nil }}
		tenant, run, q := job.Tenant, job.Run, "SELECT photo.download_build_phase($1,$2)"
		switch kind {
		case "tenant":
			tenant = uuid.New()
		case "run":
			run = uuid.New()
		case "query":
			q = "SELECT unsafe"
		case "phase":
			gate.Expected = "planning"
		}
		var out string
		if e := gate.QueryRow(context.Background(), q, tenant, run).Scan(&out); e == nil || db.calls != 0 {
			t.Fatal("closed phase query authority widened")
		}
	}
}
func TestScopedAdvanceClaimRefusalRollsBackBeforeCommit(t *testing.T) {
	tenant, run, token := uuid.New(), uuid.New(), uuid.New()
	tx := &leaseTx{row: leaseRow{values: []any{tenant, run, token, time.Now().Add(time.Minute)}}}
	called := 0
	leases := scopedLeases{allowed: map[uuid.UUID]Job{run: {Tenant: tenant, Run: run}}, begin: func(context.Context) (pgx.Tx, error) { return tx, nil }, guard: func(context.Context, pgx.Tx, *workflow.DownloadBuildLease) error { called++; return errAdvancePhase }}
	_, e := leases.Claim(context.Background())
	if !errors.Is(e, errAdvancePhase) || called != 1 || tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatal("phase mismatch leaked global claim lease")
	}
}
func TestAdvancePhaseGuardPreservesCancellation(t *testing.T) {
	job := actualAdvanceFixture(t).Jobs[0]
	db := &phaseFixtureDB{phase: "archiving"}
	called := 0
	gate := guardedPhaseDB{DB: db, Job: job, Expected: "archiving", Before: func(context.Context) error { called++; return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var phase string
	if e := gate.QueryRow(ctx, "SELECT photo.download_build_phase($1,$2)", job.Tenant, job.Run).Scan(&phase); !errors.Is(e, context.Canceled) || called != 0 {
		t.Fatal("cancellation changed phase authority")
	}
}
func TestAdvanceVolumeLimitsUseProductionParserWithoutOverride(t *testing.T) {
	s := actualAdvanceFixture(t)
	empty := func(string) string { return "" }
	if _, e := resolveAdvanceLimits(s, empty); e != nil {
		t.Fatal(e)
	}
	if _, e := resolveAdvanceLimits(s, func(k string) string {
		if k == "PHOTO_DOWNLOAD_VOLUME_FILES" {
			return "1"
		}
		return ""
	}); e == nil {
		t.Fatal("ambient limit change not bound to approval")
	}
	if _, e := resolveAdvanceLimits(s, func(k string) string {
		if k == "PHOTO_DOWNLOAD_VOLUME_BYTES" {
			return "invalid"
		}
		return ""
	}); e == nil {
		t.Fatal("production parser failure ignored")
	}
}
