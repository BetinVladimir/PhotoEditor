package queue

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RestoreReplayInstallation struct {
	Database        string
	Vhost           string
	MaximumEvents   int64
	AssertIsolation func(context.Context) error
}

func validateRestoreReplayInstallation(dsn, brokerURL string, installation RestoreReplayInstallation) (*pgxpool.Config, error) {
	if installation.Database == "" || len(installation.Database) > 63 || installation.Vhost == "" || installation.Vhost == "/" || installation.AssertIsolation == nil || installation.MaximumEvents < 1 || installation.MaximumEvents > 10_000_000 {
		return nil, errors.New("restore replay installation invalid")
	}
	broker, err := url.Parse(brokerURL)
	if err != nil || broker.Scheme != "amqps" || broker.Hostname() == "" || broker.Fragment != "" || broker.RawQuery != "" || strings.TrimPrefix(broker.Path, "/") != installation.Vhost {
		return nil, errors.New("restore replay requires exact isolated TLS vhost")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != installation.Database || cfg.ConnConfig.TLSConfig == nil || cfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("restore replay requires exact verified TLS database")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_publisher"
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.MaxConns = 2
	cfg.MinConns = 0
	return cfg, nil
}

// RunRestoreReplay owns one bounded task-only recovery publisher process.
// Installation guard must independently attest current recovery phase, exclusive
// target, paused consumers and blocked production outgoing. Config alone is not
// an isolation attestation. No topology creation, reconnect or mail dispatch.
func RunRestoreReplay(ctx context.Context, dsn, brokerURL string, installation RestoreReplayInstallation) (evidence RestoreReplayEvidence, err error) {
	return runRestoreReplay(ctx, dsn, brokerURL, installation, nil, "")
}

func runRestoreReplay(ctx context.Context, dsn, brokerURL string, installation RestoreReplayInstallation, expected *int64, expectedSHA string) (evidence RestoreReplayEvidence, err error) {
	cfg, err := validateRestoreReplayInstallation(dsn, brokerURL, installation)
	if err != nil {
		return evidence, err
	}
	if err = ctx.Err(); err != nil {
		return evidence, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err = installation.AssertIsolation(ctx); err != nil {
		return evidence, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return evidence, errors.New("restore replay database unavailable")
	}
	defer pool.Close()
	lease, err := pool.Acquire(ctx)
	if err != nil {
		return evidence, errors.New("restore replay session unavailable")
	}
	defer lease.Release()
	var database string
	var pid int32
	if err = lease.QueryRow(ctx, "SELECT current_database(),pg_backend_pid()").Scan(&database, &pid); err != nil || database != installation.Database {
		return evidence, errors.New("restore replay database identity mismatch")
	}
	var locked bool
	if err = lease.QueryRow(ctx, "SELECT pg_try_advisory_lock(1346916180,33)").Scan(&locked); err != nil || !locked {
		return evidence, errors.New("restore replay already running")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		var released bool
		if releaseErr := lease.QueryRow(cleanup, "SELECT pg_advisory_unlock(1346916180,33)").Scan(&released); releaseErr != nil || !released {
			_ = lease.Conn().Close(cleanup)
			evidence = RestoreReplayEvidence{}
			err = errors.New("restore replay lock release failed")
		}
	}()
	conn, err := dial(brokerURL)
	if err != nil {
		return evidence, errors.New("restore replay broker unavailable")
	}
	defer conn.Close()
	publisher, err := NewPublisher(conn)
	if err != nil {
		return evidence, errors.New("restore replay confirms unavailable")
	}
	defer publisher.Close()
	guard := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if conn.IsClosed() {
			return errors.New("restore replay broker closed")
		}
		if err := installation.AssertIsolation(ctx); err != nil {
			return err
		}
		var actual string
		var actualPID int32
		var owned bool
		err := lease.QueryRow(ctx, "SELECT current_database(),pg_backend_pid(),EXISTS(SELECT FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid() AND classid=1346916180 AND objid=33 AND objsubid=2 AND granted)").Scan(&actual, &actualPID, &owned)
		if err != nil || actual != database || actualPID != pid || !owned {
			return errors.New("restore replay lock ownership lost")
		}
		return ctx.Err()
	}
	outbox := Outbox{DB: pool, Publisher: publisher}
	return outbox.replayRestoredTasks(ctx, installation.Database, guard, installation.MaximumEvents, expected, expectedSHA)
}
