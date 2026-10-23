package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"
)

type failureInstallationDB struct {
	value bool
	err   error
	calls int
	t     *testing.T
}

func (d *failureInstallationDB) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	d.calls++
	if q != applyTerminalFailureInstallationSQL || !strings.Contains(q, "current_user='photo_scheduler'") || !strings.Contains(q, "to_regprocedure('photo.fail_apply_terminal_stage(uuid,uuid,uuid)')") || !strings.Contains(q, "has_function_privilege") || len(args) != 0 {
		d.t.Fatal("installation lost exact public function/role/privilege binding")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 5*time.Second {
		d.t.Fatal("unbounded installation query")
	}
	return terminalFailureRow{d.value, d.err}
}
func TestApplyTerminalFailureInstallationRefusesAbsentOrDeniedBeforeWork(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value bool
		err   error
		want  bool
	}{{"present_executable", true, nil, true}, {"missing_or_denied", false, nil, false}, {"unknown_query", false, errors.New("unavailable"), false}} {
		t.Run(tc.name, func(t *testing.T) {
			d := &failureInstallationDB{value: tc.value, err: tc.err, t: t}
			err := requireApplyTerminalFailureInstallation(context.Background(), d)
			if (err == nil) != tc.want || d.calls != 1 {
				t.Fatal("installation gate changed", err, d.calls)
			}
		})
	}
	if requireApplyTerminalFailureInstallation(context.Background(), nil) == nil {
		t.Fatal("nil installation database admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &failureInstallationDB{value: true, t: t}
	if err := requireApplyTerminalFailureInstallation(ctx, d); !errors.Is(err, context.Canceled) || d.calls != 0 {
		t.Fatal("cancelled installation touched database", err, d.calls)
	}
}
