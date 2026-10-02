package queue

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

func dial(url string) (*amqp.Connection, error) {
	return amqp.DialConfig(url, amqp.Config{Heartbeat: 30 * time.Second, Dial: amqp.DefaultDial(10 * time.Second)})
}
func Bootstrap(url string) error {
	conn, err := dial(url)
	if err != nil {
		return errors.New("broker connection unavailable")
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	return Declare(ch)
}
func RunPublisher(ctx context.Context, dsn, url, listen string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("publisher database configuration invalid")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_publisher"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("publisher database unavailable")
	}
	defer pool.Close()
	var ready atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !ready.Load() {
			w.WriteHeader(503)
			w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		pingCtx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if pool.Ping(pingCtx) != nil {
			w.WriteHeader(503)
			w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		w.Write([]byte(`{"status":"ready"}`))
	})
	server := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	defer server.Close()
	go server.Serve(listener)
	backoff := time.Second
	for ctx.Err() == nil {
		conn, e := dial(url)
		if e != nil {
			ready.Store(false)
			slog.Warn("publisher broker unavailable")
			if !pause(ctx, backoff) {
				break
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		publisher, e := NewPublisher(conn)
		if e != nil {
			conn.Close()
			ready.Store(false)
			if !pause(ctx, backoff) {
				break
			}
			continue
		}
		outbox := Outbox{DB: pool, Publisher: publisher}
		backoff = time.Second
		for ctx.Err() == nil {
			if conn.IsClosed() {
				e = errors.New("broker closed")
				break
			}
			count := 0
			for _, kind := range []string{"task", "mail"} {
				n, dispatchErr := outbox.Dispatch(ctx, kind, 10)
				count += n
				if dispatchErr != nil {
					e = dispatchErr
					break
				}
			}
			if e != nil {
				ready.Store(false)
				slog.Warn("publisher delivery deferred")
				break
			}
			ready.Store(!conn.IsClosed())
			if count == 0 && !pause(ctx, 250*time.Millisecond) {
				break
			}
		}
		ready.Store(false)
		publisher.Close()
		conn.Close()
		if !pause(ctx, time.Second) {
			break
		}
	}
	return nil
}
func pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
