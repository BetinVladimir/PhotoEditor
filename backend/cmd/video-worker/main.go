// Command video-worker runs the video job worker (ingest, render) and, optionally, the housekeeping sweeper on one host,
// for instance the GPU machine. It talks to Postgres as photo_worker (and photo_maintenance for the sweeper) and to the
// object store that the API uses.
//
//	video-worker -worker-dsn 'postgres://…' -s3 host:port,access,secret -bucket photo-video -encoder h264_nvenc
//
// Environment alternatives for secrets: VIDEO_WORKER_DATABASE_URL, VIDEO_MAINTENANCE_DATABASE_URL, VIDEO_S3.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/video/worker"
)

func env(flagValue, name string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv(name)
}

func rolePool(ctx context.Context, dsn, role string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("%s dsn: invalid", role)
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize())
		return err
	}
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("%s: %w", role, err)
	}
	return p, nil
}

func main() {
	workerDSN := flag.String("worker-dsn", "", "DSN whose connections may SET ROLE photo_worker (or VIDEO_WORKER_DATABASE_URL)")
	maintDSN := flag.String("maintenance-dsn", "", "optional DSN for photo_maintenance; starts the sweeper (or VIDEO_MAINTENANCE_DATABASE_URL)")
	s3spec := flag.String("s3", "", "endpoint,access,secret (or VIDEO_S3); without it objects are read from -objects")
	objects := flag.String("objects", "", "directory of a LocalStore (development)")
	bucket := flag.String("bucket", "photo-video", "bucket of the video objects")
	work := flag.String("work", "", "scratch directory for jobs (default: system temp)")
	ffmpeg := flag.String("ffmpeg", "ffmpeg", "ffmpeg binary")
	ffprobe := flag.String("ffprobe", "ffprobe", "ffprobe binary")
	encoder := flag.String("encoder", "libx264", "libx264 or h264_nvenc")
	idle := flag.Duration("idle", 2*time.Second, "polling period when the queue is empty")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, env(*workerDSN, "VIDEO_WORKER_DATABASE_URL"), env(*maintDSN, "VIDEO_MAINTENANCE_DATABASE_URL"), env(*s3spec, "VIDEO_S3"),
		*objects, *bucket, *work, *ffmpeg, *ffprobe, *encoder, *idle); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func run(ctx context.Context, workerDSN, maintDSN, s3spec, objects, bucket, work, ffmpeg, ffprobe, encoder string, idle time.Duration) error {
	if workerDSN == "" {
		return errors.New("a worker DSN is required")
	}
	if encoder != "libx264" && encoder != "h264_nvenc" {
		return fmt.Errorf("unknown encoder %q", encoder)
	}
	var store worker.Store
	switch {
	case s3spec != "":
		f := strings.Split(s3spec, ",")
		if len(f) < 3 {
			return errors.New("-s3 wants endpoint,access,secret")
		}
		s3, err := worker.NewS3Store(f[0], "", f[1], f[2], false)
		if err != nil {
			return err
		}
		store = s3
	case objects != "":
		store = worker.LocalStore{Root: objects}
	default:
		return errors.New("either -s3 or -objects is required")
	}
	if work == "" {
		work = filepath.Join(os.TempDir(), "video-worker")
	}
	if err := os.MkdirAll(work, 0o750); err != nil {
		return err
	}
	pool, err := rolePool(ctx, workerDSN, "photo_worker")
	if err != nil {
		return err
	}
	defer pool.Close()
	if maintDSN != "" {
		mp, err := rolePool(ctx, maintDSN, "photo_maintenance")
		if err != nil {
			return err
		}
		defer mp.Close()
		go (&worker.Sweeper{Pool: mp, Store: store}).Loop(ctx, 30*time.Second)
	}
	w := &worker.Worker{Pool: pool, Store: store, FFmpeg: ffmpeg, FFprobe: ffprobe, Encoder: encoder, Bucket: bucket, WorkDir: work}
	log.Printf("video-worker started: encoder=%s bucket=%s work=%s sweeper=%v", encoder, bucket, work, maintDSN != "")
	// A failing job must not take the process down: back off and carry on.
	for ctx.Err() == nil {
		if err := w.Loop(ctx, idle); err != nil && ctx.Err() == nil {
			log.Printf("worker loop: %v (retrying in 5s)", err)
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
		}
	}
	return ctx.Err()
}
