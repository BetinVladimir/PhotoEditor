package worker

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Sweeper is the periodic maintenance of video uploads. Pool must run as photo_maintenance.
type Sweeper struct {
	Pool *pgxpool.Pool
}

// Once returns stuck completions to 'uploading' and expires uploads past their deadline, releasing their reservations.
func (s *Sweeper) Once(ctx context.Context) (recovered, expired int, err error) {
	if err = s.Pool.QueryRow(ctx, `SELECT photo.recover_video_upload_completions(500)`).Scan(&recovered); err != nil {
		return
	}
	err = s.Pool.QueryRow(ctx, `SELECT photo.expire_video_uploads(500)`).Scan(&expired)
	return
}

// Loop runs Once every interval until ctx ends.
func (s *Sweeper) Loop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if r, e, err := s.Once(ctx); err != nil && ctx.Err() == nil {
			log.Printf("video sweeper: %v", err)
		} else if r+e > 0 {
			log.Printf("video sweeper: recovered %d completions, expired %d uploads", r, e)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
