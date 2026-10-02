package collection

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
)

func RunWorker(ctx context.Context, dsn, amqpURL, listen string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("collection worker database configuration invalid")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_worker"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "45000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("collection worker database unavailable")
	}
	defer pool.Close()
	var installed bool
	if err = pool.QueryRow(ctx, "SELECT to_regprocedure('photo.process_bulk_chunk(uuid,uuid)') IS NOT NULL").Scan(&installed); err != nil || !installed {
		return errors.New("collection worker database function unavailable")
	}
	var ready atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		check, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if !ready.Load() || pool.Ping(check) != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	defer server.Close()
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	worker := BulkWorker{DB: pool}
	for ctx.Err() == nil {
		connection, dialErr := amqp.DialConfig(amqpURL, amqp.Config{Heartbeat: 30 * time.Second, Dial: amqp.DefaultDial(10 * time.Second)})
		if dialErr != nil {
			ready.Store(false)
			if !waitWorker(ctx, time.Second) {
				break
			}
			continue
		}
		channel, channelErr := connection.Channel()
		if channelErr != nil {
			connection.Close()
			continue
		}
		if channelErr = channel.Qos(1, 0, false); channelErr != nil {
			channel.Close()
			connection.Close()
			return errors.New("collection worker QoS failed")
		}
		deliveries, consumeErr := channel.Consume("q.worker.collection", "", false, false, false, false, nil)
		if consumeErr != nil {
			channel.Close()
			connection.Close()
			if !waitWorker(ctx, time.Second) {
				break
			}
			continue
		}
		ready.Store(true)
		running := true
		for running {
			select {
			case <-ctx.Done():
				running = false
			case serveErr := <-serveErrors:
				channel.Close()
				connection.Close()
				if errors.Is(serveErr, http.ErrServerClosed) {
					return nil
				}
				return serveErr
			case delivery, ok := <-deliveries:
				if !ok {
					running = false
					continue
				}
				_, processErr := worker.Process(ctx, delivery.Body)
				if errors.Is(processErr, ErrPoisonBulkCommand) {
					if delivery.Reject(false) != nil {
						running = false
					}
					continue
				}
				if processErr != nil || delivery.Ack(false) != nil {
					ready.Store(false)
					running = false
				}
			}
		}
		ready.Store(false)
		channel.Close()
		connection.Close()
		if !waitWorker(ctx, time.Second) {
			break
		}
	}
	return nil
}

func waitWorker(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
