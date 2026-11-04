package worker

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Sweeper is the periodic maintenance of video uploads. Pool must run as photo_maintenance.
type Sweeper struct {
	Pool  *pgxpool.Pool
	Store Store // when set, withdrawn media are removed from it and their quota released
}

// Once returns stuck completions to 'uploading', expires uploads past their deadline (releasing their reservations),
// rejects abandoned ingests, folds old autosaves and, with a Store, finishes withdrawn media.
func (s *Sweeper) Once(ctx context.Context) (recovered, expired int, err error) {
	if err = s.Pool.QueryRow(ctx, `SELECT photo.recover_video_upload_completions(500)`).Scan(&recovered); err != nil {
		return
	}
	if err = s.Pool.QueryRow(ctx, `SELECT photo.expire_video_uploads(500)`).Scan(&expired); err != nil {
		return
	}
	// Housekeeping that changes nothing a user sees: abandoned ingests are rejected (counted with recovered), old autosaves folded.
	var reconciled int
	if err = s.Pool.QueryRow(ctx, `SELECT photo.reconcile_video_stuck(500)`).Scan(&reconciled); err != nil {
		return
	}
	recovered += reconciled
	var leases int
	if err = s.Pool.QueryRow(ctx, `SELECT photo.expire_video_egress_leases(500)`).Scan(&leases); err != nil {
		return
	}
	var compacted int
	if err = s.Pool.QueryRow(ctx, `SELECT photo.compact_video_autosaves(500)`).Scan(&compacted); err != nil {
		return
	}
	var purged int
	if err = s.Pool.QueryRow(ctx, `SELECT photo.purge_video_history(500)`).Scan(&purged); err != nil {
		return
	}
	if s.Store != nil {
		err = s.deleteWithdrawn(ctx)
	}
	return
}

// deleteWithdrawn is phase two of FN-D-13: objects first, quota only after every object is gone.
func (s *Sweeper) deleteWithdrawn(ctx context.Context) error {
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `SELECT photo.list_video_media_deletions(100)::text`).Scan(&raw); err != nil {
		return err
	}
	var todo []struct {
		Tenant  string `json:"tenant_id"`
		Media   string `json:"media_id"`
		Objects []struct {
			Bucket string `json:"bucket"`
			Key    string `json:"object_key"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(raw, &todo); err != nil {
		return err
	}
	for _, m := range todo {
		for _, o := range m.Objects {
			if err := s.Store.Remove(ctx, o.Bucket, o.Key); err != nil {
				return err
			}
		}
		var ok bool
		if err := s.Pool.QueryRow(ctx, `SELECT photo.finish_video_media_delete($1,$2)`, m.Tenant, m.Media).Scan(&ok); err != nil {
			return err
		}
	}
	return nil
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
