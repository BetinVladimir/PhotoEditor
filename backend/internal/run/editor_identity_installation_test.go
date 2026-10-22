package run

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

type editorIdentityInstallationRow struct {
	ready bool
	err   error
}

func (r editorIdentityInstallationRow) Scan(values ...any) error {
	if r.err != nil {
		return r.err
	}
	*(values[0].(*bool)) = r.ready
	return nil
}

type editorIdentityInstallationDB struct {
	ready bool
	err   error
	calls int
	t     *testing.T
}

func (d *editorIdentityInstallationDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	d.calls++
	if sql != editorIdentityInstallationSQL || len(args) != 0 {
		d.t.Fatal("installation query changed")
	}
	return editorIdentityInstallationRow{d.ready, d.err}
}
func TestEditorIdentityInstallationAuthorityOutcomes(t *testing.T) {
	sentinel := errors.New("catalog unavailable")
	for _, c := range []struct {
		name  string
		ready bool
		err   error
		want  bool
	}{{"installed", true, nil, true}, {"refused", false, nil, false}, {"query error", false, sentinel, false}} {
		t.Run(c.name, func(t *testing.T) {
			db := &editorIdentityInstallationDB{ready: c.ready, err: c.err, t: t}
			e := RequireEditorIdentityInstallation(context.Background(), db)
			if (e == nil) != c.want || db.calls != 1 {
				t.Fatal("installation outcome", e, db.calls)
			}
			if c.err != nil && !errors.Is(e, c.err) {
				t.Fatal("original catalog error lost")
			}
		})
	}
	if RequireEditorIdentityInstallation(context.Background(), nil) == nil {
		t.Fatal("nil database accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(RequireEditorIdentityInstallation(ctx, &editorIdentityInstallationDB{ready: true, t: t}), context.Canceled) {
		t.Fatal("cancellation lost")
	}
}
