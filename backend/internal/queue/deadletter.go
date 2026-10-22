package queue

import (
 "photoeditor/backend/internal/database"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"photoeditor/backend/internal/canonical"
)

type deadCommand struct {
	SchemaVersion    int       `json:"schema_version"`
	EventID          uuid.UUID `json:"event_id"`
	TaskID           uuid.UUID `json:"task_id"`
	Attempt          int       `json:"attempt"`
	Operation        string    `json:"operation"`
	OperationVersion string    `json:"operation_version"`
	ResourceClass    string    `json:"resource_class"`
}

type DeadLetterRecord struct {
	TaskID, EventID *uuid.UUID
	WorkerClass     string
	RoutingKey      string
	PayloadSHA256   string
	Metadata        json.RawMessage
}

func DecodeDeadLetter(raw []byte, routing string) (DeadLetterRecord, error) {
	if len(raw) == 0 || len(raw) > 65536 || len(routing) < 1 || len(routing) > 160 {
		return DeadLetterRecord{}, errors.New("invalid dead letter")
	}
	sum := sha256.Sum256(raw)
	class := strings.TrimPrefix(routing, "dead.")
	switch class {
	case "cpu", "gpu", "media", "archive", "collection", "mail":
	default:
		class = "unknown"
	}
	record := DeadLetterRecord{WorkerClass: class, RoutingKey: routing, PayloadSHA256: hex.EncodeToString(sum[:])}
	canonicalBody, err := canonical.Bytes(json.RawMessage(raw))
	if err != nil {
		record.Metadata = json.RawMessage(`{"schema_version":1,"parse_status":"invalid_json"}`)
		return record, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(canonicalBody, &fields) != nil {
		record.Metadata = json.RawMessage(`{"schema_version":1,"parse_status":"invalid_command"}`)
		return record, nil
	}
	allowed := map[string]bool{"schema_version": true, "event_id": true, "tenant_id": true, "run_id": true, "task_id": true, "attempt": true, "operation": true, "operation_version": true, "resource_class": true, "correlation_id": true, "inputs": true, "params": true, "model": true, "timeout_seconds": true, "traceparent": true}
	if len(fields) != len(allowed) {
		record.Metadata = json.RawMessage(`{"schema_version":1,"parse_status":"invalid_command"}`)
		return record, nil
	}
	for name := range fields {
		if !allowed[name] {
			record.Metadata = json.RawMessage(`{"schema_version":1,"parse_status":"invalid_command"}`)
			return record, nil
		}
	}
	var command deadCommand
	if json.Unmarshal(canonicalBody, &command) != nil || command.SchemaVersion != 1 || command.EventID == uuid.Nil || command.TaskID == uuid.Nil || command.Attempt < 1 || len(command.Operation) < 1 || len(command.Operation) > 120 || command.OperationVersion != "1" || command.ResourceClass != class {
		record.Metadata = json.RawMessage(`{"schema_version":1,"parse_status":"invalid_command"}`)
		return record, nil
	}
	record.TaskID, record.EventID = &command.TaskID, &command.EventID
	record.Metadata, _ = canonical.Marshal(map[string]any{"schema_version": 1, "parse_status": "valid", "attempt": command.Attempt, "operation": command.Operation, "operation_version": command.OperationVersion, "resource_class": command.ResourceClass})
	return record, nil
}

func recordDeadLetter(ctx context.Context, db *pgxpool.Pool, record DeadLetterRecord) error {
	var id *uuid.UUID
	err := db.QueryRow(ctx, `SELECT photo.record_dead_letter($1,$2,$3,$4,$5,$6)`, record.TaskID, record.EventID, record.WorkerClass, record.RoutingKey, record.PayloadSHA256, record.Metadata).Scan(&id)
	if err != nil {
		return err
	}
	if id == nil || *id == uuid.Nil {
		return errors.New("dead letter rejected")
	}
	return nil
}

// RunDeadLetterCollector consumes every bounded DLQ. It persists only a digest
// and an allowlisted projection, then acknowledges the broker delivery.
func RunDeadLetterCollector(ctx context.Context, dsn, url, listen string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("dead-letter database configuration invalid")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_publisher"
	database.GuardApplicationRole(cfg)
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("dead-letter database unavailable")
	}
	defer db.Close()
	if err = database.RequireApplicationRole(ctx, db, "photo_publisher"); err != nil { return err }
	conn, err := dial(url)
	if err != nil {
		return errors.New("dead-letter broker unavailable")
	}
	defer conn.Close()
	classes := []string{"cpu", "gpu", "media", "archive", "collection", "mail"}
	var wg sync.WaitGroup
	errCh := make(chan error, len(classes)+1)
	var ready atomic.Bool
	for _, class := range classes {
		channel, channelErr := conn.Channel()
		if channelErr != nil {
			return channelErr
		}
		deliveries, consumeErr := channel.Consume("q.dead."+class, "dead-letter-"+class, false, false, false, false, nil)
		if consumeErr != nil {
			channel.Close()
			return consumeErr
		}
		wg.Add(1)
		go func(ch *amqp.Channel, stream <-chan amqp.Delivery) {
			defer wg.Done()
			defer ch.Close()
			for {
				select {
				case <-ctx.Done():
					return
				case delivery, ok := <-stream:
					if !ok {
						errCh <- errors.New("dead-letter consumer closed")
						return
					}
					record, decodeErr := DecodeDeadLetter(delivery.Body, delivery.RoutingKey)
					if decodeErr != nil || recordDeadLetter(ctx, db, record) != nil {
						_ = delivery.Nack(false, true)
						continue
					}
					if ackErr := delivery.Ack(false); ackErr != nil {
						errCh <- ackErr
						return
					}
				}
			}
		}(channel, deliveries)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		probe, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if !ready.Load() || conn.IsClosed() || db.Ping(probe) != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	server := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		conn.Close()
		wg.Wait()
		return err
	}
	defer listener.Close()
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()
	ready.Store(true)
	select {
	case <-ctx.Done():
		ready.Store(false)
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = server.Shutdown(shutdown)
		cancel()
		conn.Close()
		wg.Wait()
		return nil
	case err = <-errCh:
		ready.Store(false)
		_ = server.Close()
		conn.Close()
		wg.Wait()
		return err
	}
}
