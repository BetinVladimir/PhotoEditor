package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

type terminalFailureStore struct {
	ApplyPhaseStore // Unused publication paths must panic, never silently succeed.
	lease           *ApplyLease
	failed          bool
	err             error
	calls           []string
	stop            error
}

func (s *terminalFailureStore) Claim(context.Context) (*ApplyLease, error) {
	s.calls = append(s.calls, "claim")
	return s.lease, nil
}
func (s *terminalFailureStore) FailTerminalPredecessor(context.Context, ApplyLease) (bool, error) {
	s.calls = append(s.calls, "terminal_failure")
	return s.failed, s.err
}
func (s *terminalFailureStore) Outputs(context.Context, ApplyLease, string, string) ([]applyOutput, error) {
	s.calls = append(s.calls, "outputs")
	return nil, s.stop
}
func TestApplyTerminalFailureBeforePublication(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    int
		failed     bool
		failureErr error
		wantWorked bool
		wantCalls  int
	}{
		{"terminal", 4, true, nil, true, 2},
		{"not_terminal", 4, false, nil, false, 3},
		{"gate_error", 4, false, errors.New("failure gate unavailable"), false, 2},
		{"legacy_unchanged", 3, false, nil, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stop := errors.New("existing downstream path")
			s := &terminalFailureStore{lease: &ApplyLease{GraphVersion: tc.version, Phase: "adopt"}, failed: tc.failed, err: tc.failureErr, stop: stop}
			worked, err := (ApplyCoordinator{Store: s}).ProcessOne(context.Background())
			if worked != tc.wantWorked || len(s.calls) != tc.wantCalls {
				t.Fatalf("wrong authority path: worked=%v calls=%v", worked, s.calls)
			}
			if tc.failed {
				if err != nil {
					t.Fatal(err)
				}
			} else if tc.failureErr != nil {
				if !errors.Is(err, tc.failureErr) {
					t.Fatal("gate error lost", err)
				}
			} else if !errors.Is(err, stop) {
				t.Fatal("existing downstream result lost", err)
			}
			if tc.version != 4 {
				for _, c := range s.calls {
					if c == "terminal_failure" {
						t.Fatal("legacy failure semantics changed")
					}
				}
			}
		})
	}
}
func TestApplyNoClaimNoTerminalFailureCall(t *testing.T) {
	s := &terminalFailureStore{}
	worked, err := (ApplyCoordinator{Store: s}).ProcessOne(context.Background())
	if worked || err != nil || len(s.calls) != 1 {
		t.Fatal("nil lease performed work", worked, err, s.calls)
	}
}

type terminalFailureRow struct {
	value bool
	err   error
}

func (r terminalFailureRow) Scan(values ...any) error {
	if r.err != nil {
		return r.err
	}
	*(values[0].(*bool)) = r.value
	return nil
}

type terminalFailureDB struct {
	styleAnalysisDB
	lease ApplyLease
	calls int
	query string
	args  []any
	value bool
	err   error
}

func (d *terminalFailureDB) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	d.calls++
	d.query = q
	d.args = args
	return terminalFailureRow{d.value, d.err}
}
func TestApplyTerminalFailureStoreUsesExactLeaseAuthority(t *testing.T) {
	l := ApplyLease{TenantID: uuid.New(), RunID: uuid.New(), Token: uuid.New(), GraphVersion: 4}
	for _, result := range []bool{true, false} {
		d := &terminalFailureDB{lease: l, value: result}
		got, err := (ApplyStore{DB: d}).FailTerminalPredecessor(context.Background(), l)
		if got != result || err != nil || d.calls != 1 || d.query != `SELECT photo.fail_apply_terminal_stage($1,$2,$3)` || len(d.args) != 3 || d.args[0] != l.TenantID || d.args[1] != l.RunID || d.args[2] != l.Token {
			t.Fatal("lease binding changed", d.calls, d.query, d.args, err)
		}
	}
	e := errors.New("database refusal")
	d := &terminalFailureDB{err: e}
	if _, err := (ApplyStore{DB: d}).FailTerminalPredecessor(context.Background(), l); !errors.Is(err, e) {
		t.Fatal("SQL refusal lost", err)
	}
	for _, bad := range []ApplyLease{{}, {TenantID: l.TenantID, RunID: l.RunID, Token: l.Token, GraphVersion: 3}, {TenantID: l.TenantID, RunID: l.RunID, GraphVersion: 4}} {
		d := &terminalFailureDB{}
		if _, err := (ApplyStore{DB: d}).FailTerminalPredecessor(context.Background(), bad); err == nil || d.calls != 0 {
			t.Fatal("invalid authority reached SQL")
		}
	}
	if _, err := (ApplyStore{}).FailTerminalPredecessor(context.Background(), l); err == nil {
		t.Fatal("nil DB admitted")
	}
}

func TestOperatorLedgerTerminalFailurePortIsScopedAndNeverFabricatesFailure(t *testing.T) {
	index := 0
	l := ApplyLease{TenantID: uuid.New(), RunID: uuid.New(), Token: uuid.New(), GraphVersion: 4, Phase: "resolve", ItemCount: 3, StageIndex: &index}
	style := uuid.New()
	l.StyleVersionID = &style
	ledger := &OperatorApplyLedger{lease: l}
	copyStyle := style
	l.StyleVersionID = &copyStyle
	if failed, err := ledger.FailTerminalPredecessor(context.Background(), l); failed || err != nil {
		t.Fatal("isolated measurement invented terminal failure", failed, err)
	}
	for _, change := range []string{"tenant", "run", "token", "phase", "index", "version", "failed", "complete"} {
		bad := l
		current := &OperatorApplyLedger{lease: l}
		switch change {
		case "tenant":
			bad.TenantID = uuid.New()
		case "run":
			bad.RunID = uuid.New()
		case "token":
			bad.Token = uuid.New()
		case "phase":
			bad.Phase = "quality"
		case "index":
			i := 1
			bad.StageIndex = &i
		case "version":
			bad.GraphVersion = 3
		case "failed":
			current.failed = true
		case "complete":
			current.complete = true
		}
		if failed, err := current.FailTerminalPredecessor(context.Background(), bad); failed || err == nil {
			t.Fatal("foreign/terminal measurement scope accepted", change)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if failed, err := ledger.FailTerminalPredecessor(ctx, l); failed || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled measurement ignored", failed, err)
	}
}
