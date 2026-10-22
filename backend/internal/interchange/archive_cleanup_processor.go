package interchange

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"time"
)

var ErrArchiveCleanupLeaseLost = errors.New("archive cleanup lease lost")

type ArchiveCleanupObjects interface {
	PurgeInterchangeArchiveKey(context.Context, uuid.UUID, uuid.UUID, string) error
}

// AssertQuiescent must attest that upload URL authority has expired/revoked and
// every server-side writer is fenced/drained for this exact upload and bucket.
// A timer alone is insufficient. The guard is mandatory, including before Finish.
type ArchiveCleanupWriters interface {
	AssertQuiescent(context.Context, ArchiveCleanupLease) error
}
type ArchiveCleanupProcessor struct {
	Leases                                       ArchiveCleanupLeases
	Objects                                      ArchiveCleanupObjects
	Writers                                      ArchiveCleanupWriters
	HeartbeatInterval, RenewTimeout, WorkTimeout time.Duration
}

func (p ArchiveCleanupProcessor) ProcessOne(ctx context.Context) (bool, error) {
	interval, renewTimeout, workTimeout := p.HeartbeatInterval, p.RenewTimeout, p.WorkTimeout
	if interval == 0 {
		interval = 10 * time.Second
	}
	if renewTimeout == 0 {
		renewTimeout = 5 * time.Second
	}
	if workTimeout == 0 {
		workTimeout = 10 * time.Minute
	}
	if p.Leases == nil || p.Objects == nil || p.Writers == nil || interval <= 0 || interval > 10*time.Second || renewTimeout <= 0 || renewTimeout > 5*time.Second || workTimeout <= 0 || workTimeout > 10*time.Minute {
		return false, v2input.ErrInvalid
	}
	claim, cancel := context.WithTimeout(ctx, 5*time.Second)
	borrowed, err := p.Leases.Claim(claim)
	cancel()
	if borrowed == nil {
		return false, err
	}
	lease := *borrowed
	committed := false
	defer func() {
		if !committed {
			release, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer done()
			_, _ = p.Leases.Release(release, lease)
		}
	}()
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, err = archiveStorageGeneration(lease.Tenant, lease.Upload, lease.Key)
	if err != nil || lease.Token == uuid.Nil || len(lease.Bucket) < 3 || len(lease.Bucket) > 63 || lease.Until.IsZero() || lease.NotBefore.IsZero() {
		return false, v2input.ErrInvalid
	}
	work, stop := context.WithTimeout(ctx, workTimeout)
	defer stop()
	work, lose := context.WithCancelCause(work)
	defer lose(nil)
	renew := func() error {
		check, done := context.WithTimeout(work, renewTimeout)
		defer done()
		ok, err := p.Leases.Renew(check, lease)
		if err != nil {
			return err
		}
		if !ok {
			return ErrArchiveCleanupLeaseLost
		}
		return nil
	}
	if err := renew(); err != nil {
		return false, err
	}
	heartbeat, stopHeartbeat := context.WithCancel(work)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeat.Done():
				return
			case <-ticker.C:
				if err := renew(); err != nil {
					lose(err)
					return
				}
			}
		}
	}()
	// Join before the deferred lease release, even if an object-store adapter panics.
	defer func() { stopHeartbeat(); <-joined }()
	ioErr := p.Writers.AssertQuiescent(work, lease)
	if ioErr == nil {
		if cause := context.Cause(work); cause != nil {
			return false, cause
		}
		ioErr = p.Objects.PurgeInterchangeArchiveKey(work, lease.Tenant, lease.Upload, lease.Key)
	}
	if ioErr == nil {
		ioErr = p.Writers.AssertQuiescent(work, lease)
	}
	stopHeartbeat()
	<-joined
	if cause := context.Cause(work); cause != nil {
		return false, cause
	}
	if ioErr != nil {
		return false, ioErr
	}
	if err := renew(); err != nil {
		return false, err
	}
	if cause := context.Cause(work); cause != nil {
		return false, cause
	}
	finish, done := context.WithTimeout(work, 5*time.Second)
	accepted, err := p.Leases.Finish(finish, lease)
	done()
	if err != nil {
		return false, err
	}
	if !accepted {
		return false, ErrArchiveCleanupLeaseLost
	}
	if cause := context.Cause(work); cause != nil {
		return false, cause
	}
	committed = true
	return true, nil
}
