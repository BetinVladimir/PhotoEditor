package mail

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"log/slog"
	"net"
	"net/http"
	"os"
	"photoeditor/backend/internal/auth"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

func ConfigFromEnv(development bool) (Config, error) {
	c := Config{Host: os.Getenv("SMTP_HOST"), From: os.Getenv("SMTP_FROM"), Username: os.Getenv("SMTP_USERNAME"), Password: os.Getenv("SMTP_PASSWORD"), TLSMode: os.Getenv("SMTP_TLS_MODE"), Development: development}
	var err error
	c.Port, err = strconv.Atoi(os.Getenv("SMTP_PORT"))
	if err != nil {
		return c, errors.New("SMTP_PORT required")
	}
	if c.TLSMode == "" {
		c.TLSMode = "starttls"
	}
	if path := os.Getenv("SMTP_PASSWORD_FILE"); path != "" {
		if c.Password != "" {
			return c, errors.New("SMTP password env and file are mutually exclusive")
		}
		b, e := os.ReadFile(path)
		if e != nil || len(b) > 4096 {
			return c, errors.New("SMTP password file unavailable")
		}
		c.Password = strings.TrimSuffix(string(b), "\n")
	}
	return c, c.Validate()
}
func Run(ctx context.Context, dsn, url, listen string, keys auth.KeyRing, sender Sender) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("mail database configuration invalid")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_mail"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("mail database unavailable")
	}
	defer pool.Close()
	worker := Worker{DB: pool, ID: uuid.New(), Keys: keys, Sender: sender}
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
		w.Write([]byte(`{"status":"ready"}`))
	})
	server := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	defer server.Close()
	go server.Serve(listener)
	for ctx.Err() == nil {
		conn, e := amqp.DialConfig(url, amqp.Config{Heartbeat: 30 * time.Second, Dial: amqp.DefaultDial(10 * time.Second)})
		if e != nil {
			ready.Store(false)
			if !wait(ctx, time.Second) {
				break
			}
			continue
		}
		ch, e := conn.Channel()
		if e != nil {
			conn.Close()
			if !wait(ctx, time.Second) {
				break
			}
			continue
		}
		if e = ch.Qos(1, 0, false); e != nil {
			ch.Close()
			conn.Close()
			return errors.New("mail QoS failed")
		}
		deliveries, e := ch.Consume("q.worker.mail", worker.ID.String(), false, false, false, false, nil)
		if e != nil {
			ch.Close()
			conn.Close()
			if !wait(ctx, time.Second) {
				break
			}
			continue
		}
		ticker := time.NewTicker(5 * time.Second)
		running := true
		for running {
			select {
			case <-ctx.Done():
				running = false
			case <-ticker.C:
				if _, e = pool.Exec(ctx, "SELECT photo.reconcile_mail(100)"); e != nil {
					ready.Store(false)
				} else {
					ready.Store(!conn.IsClosed())
				}
			case delivery, ok := <-deliveries:
				if !ok {
					running = false
					break
				}
				e = worker.Process(ctx, delivery.Body)
				if errors.Is(e, ErrPoison) {
					if e = delivery.Reject(false); e != nil {
						running = false
					}
					continue
				}
				if e != nil {
					ready.Store(false)
					slog.Warn("mail delivery deferred")
					running = false
					break
				}
				if e = delivery.Ack(false); e != nil {
					running = false
				} else {
					ready.Store(true)
				}
			}
		}
		ticker.Stop()
		ready.Store(false)
		ch.Close()
		conn.Close()
		if !wait(ctx, time.Second) {
			break
		}
	}
	return nil
}
func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
