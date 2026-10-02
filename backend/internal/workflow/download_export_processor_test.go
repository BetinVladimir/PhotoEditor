package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
	"time"
)

type downloadLeaseFake struct {
	empty    bool
	renew    bool
	releases int
}

func (f *downloadLeaseFake) Claim(context.Context) (*DownloadBuildLease, error) {
	if f.empty {
		return nil, nil
	}
	return &DownloadBuildLease{TenantID: uuid.New(), RunID: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute)}, nil
}
func (f *downloadLeaseFake) Renew(context.Context, DownloadBuildLease) (bool, error) {
	return f.renew, nil
}
func (f *downloadLeaseFake) Release(context.Context, DownloadBuildLease) (bool, error) {
	f.releases++
	return true, nil
}

type downloadBuildFunc func(context.Context, uuid.UUID, uuid.UUID) (bool, error)

func (f downloadBuildFunc) Build(c context.Context, t, r uuid.UUID) (bool, error) { return f(c, t, r) }

func TestDownloadBuildProcessorIdleSuccessAndLostLease(t *testing.T) {
	idle := &downloadLeaseFake{empty: true, renew: true}
	called := false
	p := DownloadBuildProcessor{Leases: idle, Runner: downloadBuildFunc(func(context.Context, uuid.UUID, uuid.UUID) (bool, error) { called = true; return true, nil })}
	if changed, err := p.ProcessOne(context.Background()); err != nil || changed || called {
		t.Fatal(changed, err)
	}
	ok := &downloadLeaseFake{renew: true}
	p.Leases = ok
	if changed, err := p.ProcessOne(context.Background()); err != nil || !changed || ok.releases != 1 {
		t.Fatal(changed, err, ok.releases)
	}
	lost := &downloadLeaseFake{}
	p.Leases = lost
	if _, err := p.ProcessOne(context.Background()); !errors.Is(err, ErrDownloadBuildLeaseLost) || lost.releases != 1 {
		t.Fatal(err, lost.releases)
	}
}

func TestSQLDownloadLeaseTokenBinding(t *testing.T) {
	q := &expansionQueries{row: expansionRow{values: []any{true}}}
	s := SQLDownloadBuildLeases{DB: q}
	l := DownloadBuildLease{RunID: uuid.New(), Token: uuid.New()}
	if ok, err := s.Renew(context.Background(), l); err != nil || !ok || q.args[0] != l.RunID || q.args[1] != l.Token {
		t.Fatal(err, q.args)
	}
}
