package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
)

type DownloadBuildLease struct {
	TenantID, RunID, Token uuid.UUID
	Until                  time.Time
}
type SQLDownloadBuildLeases struct{ DB artifactjson.RowQuerier }

func (s SQLDownloadBuildLeases) Claim(ctx context.Context) (*DownloadBuildLease, error) {
	if s.DB == nil {
		return nil, errors.New("download database required")
	}
	l := new(DownloadBuildLease)
	err := s.DB.QueryRow(ctx, "SELECT tenant_id,run_id,lease_token,lease_until FROM photo.claim_download_build()").Scan(&l.TenantID, &l.RunID, &l.Token, &l.Until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if l.TenantID == uuid.Nil || l.RunID == uuid.Nil || l.Token == uuid.Nil || l.Until.IsZero() {
		return nil, errors.New("invalid download build lease")
	}
	return l, nil
}
func (s SQLDownloadBuildLeases) Renew(ctx context.Context, l DownloadBuildLease) (bool, error) {
	return s.update(ctx, "SELECT photo.renew_download_build($1,$2)", l)
}
func (s SQLDownloadBuildLeases) Release(ctx context.Context, l DownloadBuildLease) (bool, error) {
	return s.update(ctx, "SELECT photo.release_download_build($1,$2)", l)
}
func (s SQLDownloadBuildLeases) update(ctx context.Context, q string, l DownloadBuildLease) (bool, error) {
	if s.DB == nil || l.RunID == uuid.Nil || l.Token == uuid.Nil {
		return false, errors.New("invalid download build lease")
	}
	var ok bool
	err := s.DB.QueryRow(ctx, q, l.RunID, l.Token).Scan(&ok)
	return ok, err
}

var ErrDownloadBuildLeaseLost = errors.New("download build lease lost")

type DownloadBuildLeases interface {
	Claim(context.Context) (*DownloadBuildLease, error)
	Renew(context.Context, DownloadBuildLease) (bool, error)
	Release(context.Context, DownloadBuildLease) (bool, error)
}
type DownloadBuildRunner interface {
	Build(context.Context, uuid.UUID, uuid.UUID) (bool, error)
}
type DownloadBuildProcessor struct {
	Leases                                       DownloadBuildLeases
	Runner                                       DownloadBuildRunner
	HeartbeatInterval, RenewTimeout, WorkTimeout time.Duration
}

func (p DownloadBuildProcessor) ProcessOne(ctx context.Context) (bool, error) {
	interval, timeout, budget := p.HeartbeatInterval, p.RenewTimeout, p.WorkTimeout
	if interval == 0 {
		interval = 10 * time.Second
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if budget == 0 {
		budget = 5 * time.Minute
	}
	if p.Leases == nil || p.Runner == nil || interval <= 0 || interval > 10*time.Second || timeout <= 0 || timeout > 5*time.Second || budget <= 0 || budget > 5*time.Minute {
		return false, errors.New("invalid download build processor")
	}
	claim, cancelClaim := context.WithTimeout(ctx, 5*time.Second)
	lease, err := p.Leases.Claim(claim)
	cancelClaim()
	if err != nil || lease == nil {
		return false, err
	}
	defer func() {
		release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = p.Leases.Release(release, *lease)
	}()
	work, cancelBudget := context.WithTimeout(ctx, budget)
	defer cancelBudget()
	work, cancelWork := context.WithCancelCause(work)
	defer cancelWork(nil)
	renew := func(c context.Context) error {
		check, cancel := context.WithTimeout(c, timeout)
		defer cancel()
		ok, e := p.Leases.Renew(check, *lease)
		if e != nil {
			return e
		}
		if !ok {
			return ErrDownloadBuildLeaseLost
		}
		return nil
	}
	if err = renew(work); err != nil {
		return false, err
	}
	heartbeat, stop := context.WithCancel(work)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-heartbeat.Done():
				return
			case <-tick.C:
				if e := renew(heartbeat); e != nil {
					cancelWork(e)
					return
				}
			}
		}
	}()
	changed, err := p.Runner.Build(work, lease.TenantID, lease.RunID)
	stop()
	<-joined
	if cause := context.Cause(work); cause != nil {
		return false, cause
	}
	return changed, err
}
