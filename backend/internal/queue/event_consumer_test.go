package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	amqp "github.com/rabbitmq/amqp091-go"
	"strings"
	"testing"
	"time"
)

func fixtureEvent(kind string) []byte {
	m := map[string]any{"schema_version": 1, "event_id": "11111111-1111-4111-8111-111111111111", "tenant_id": "22222222-2222-4222-8222-222222222222", "run_id": "33333333-3333-4333-8333-333333333333", "type": kind}
	if strings.HasPrefix(kind, "task.") {
		m["task_id"] = "44444444-4444-4444-8444-444444444444"
		m["attempt"] = 1
		m["fencing_token"] = 2
		m["outputs"] = []any{}
		m["metrics"] = map[string]any{"duration_ms": 1, "peak_memory_bytes": 2}
		m["error"] = nil
		if kind == "task.progress" {
			m["metrics"] = map[string]any{"duration_ms": 1, "peak_memory_bytes": 0, "done": 1, "total": 10}
		}
		if kind == "task.failed" || kind == "task.cancelled" {
			m["error"] = map[string]any{"code": "INTERNAL_ERROR", "message_key": "processing.failed", "retryable": false, "args": map[string]any{}}
		}
	}
	if kind == "review.created" {
		m["review_id"] = "55555555-5555-4555-8555-555555555555"
		m["kind"] = "render.compose"
	}
	if kind == "download_status" {
		m["download_job_id"] = "66666666-6666-4666-8666-666666666666"
		m["status"] = "ready"
	}
	b, _ := json.Marshal(m)
	return b
}

const eventFixtureID = "11111111-1111-4111-8111-111111111111"

func TestOrchestratorActualProducerShapes(t *testing.T) {
	for _, kind := range []string{"task.succeeded", "task.failed", "task.cancelled", "task.progress", "download_status", "review.created"} {
		t.Run(kind, func(t *testing.T) {
			e, err := DecodeOrchestratorEvent(fixtureEvent(kind), kind, eventFixtureID)
			if err != nil || e.Type != kind {
				t.Fatal(err)
			}
		})
	}
}
func TestOrchestratorContractRejections(t *testing.T) {
	raw := string(fixtureEvent("task.succeeded"))
	for name, b := range map[string]string{"duplicate": strings.Replace(raw, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1), "unknown": strings.Replace(raw, `"schema_version":1`, `"secret":"marker","schema_version":1`, 1), "null-required-number": strings.Replace(raw, `"duration_ms":1`, `"duration_ms":null`, 1), "negative": strings.Replace(raw, `"attempt":1`, `"attempt":0`, 1), "fraction": strings.Replace(raw, `"fencing_token":2`, `"fencing_token":2.5`, 1), "nonfinite": strings.Replace(raw, `"duration_ms":1`, `"duration_ms":NaN`, 1), "huge": strings.Repeat("x", 65537)} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeOrchestratorEvent([]byte(b), "task.succeeded", eventFixtureID); err == nil {
				t.Fatal("accepted invalid")
			}
		})
	}
	if _, err := DecodeOrchestratorEvent([]byte(raw), "task.failed", eventFixtureID); err == nil {
		t.Fatal("routing mismatch")
	}
	if _, err := DecodeOrchestratorEvent([]byte(raw), "task.succeeded", uuid.NewString()); err == nil {
		t.Fatal("message identity mismatch")
	}
}

type eventAck struct {
	acked, rejected bool
	requeue         bool
	err             error
}

func (a *eventAck) Ack(_ uint64, multiple bool) error {
	if multiple {
		panic("multiple ACK")
	}
	a.acked = true
	return a.err
}
func (a *eventAck) Nack(_ uint64, _, _ bool) error { panic("no hot NACK") }
func (a *eventAck) Reject(_ uint64, requeue bool) error {
	a.rejected = true
	a.requeue = requeue
	return a.err
}

type eventAccept struct {
	out   string
	err   error
	calls int
}

func (a *eventAccept) Accept(context.Context, OrchestratorEvent) (string, error) {
	a.calls++
	return a.out, a.err
}
func TestEventDeliveryCommitBoundary(t *testing.T) {
	for _, out := range []string{"accepted", "duplicate", "stale", "rejected", "unknown"} {
		t.Run(out, func(t *testing.T) {
			ack := &eventAck{}
			repo := &eventAccept{out: out}
			err := ProcessEventDelivery(context.Background(), repo, amqp.Delivery{Acknowledger: ack, Body: fixtureEvent("review.created"), RoutingKey: "review.created", MessageId: eventFixtureID})
			if out == "unknown" {
				if err == nil || ack.acked || ack.rejected {
					t.Fatal("unknown outcome acknowledged")
				}
				return
			}
			if err != nil || ack.acked != (out != "rejected") || ack.rejected != (out == "rejected") || ack.requeue {
				t.Fatal("wrong ACK boundary")
			}
		})
	}
	ack := &eventAck{}
	repo := &eventAccept{err: errors.New("database unavailable")}
	if ProcessEventDelivery(context.Background(), repo, amqp.Delivery{Acknowledger: ack, Body: fixtureEvent("review.created"), RoutingKey: "review.created", MessageId: eventFixtureID}) == nil || ack.acked || ack.rejected {
		t.Fatal("unknown commit must remain unacked")
	}
	ack = &eventAck{}
	repo = &eventAccept{}
	if ProcessEventDelivery(context.Background(), repo, amqp.Delivery{Acknowledger: ack, Body: []byte("secret-marker")}) != nil || repo.calls != 0 || !ack.rejected || ack.requeue {
		t.Fatal("poison disposition")
	}
}

type eventRow struct {
	out string
	err error
}

func (r eventRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*string)) = r.out
	return nil
}

type eventTx struct {
	pgx.Tx
	committed, rolled   bool
	queryErr, commitErr error
}

func (t *eventTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return eventRow{"accepted", t.queryErr}
}
func (t *eventTx) Commit(context.Context) error   { t.committed = true; return t.commitErr }
func (t *eventTx) Rollback(context.Context) error { t.rolled = true; return nil }

type eventBegin struct {
	tx  *eventTx
	err error
}

func (b eventBegin) Begin(context.Context) (pgx.Tx, error) { return b.tx, b.err }
func TestEventRepositoryRollbackAndUnknownCommit(t *testing.T) {
	for _, stage := range []string{"ok", "query", "commit"} {
		t.Run(stage, func(t *testing.T) {
			tx := &eventTx{}
			if stage == "query" {
				tx.queryErr = errors.New("query")
			}
			if stage == "commit" {
				tx.commitErr = errors.New("unknown commit")
			}
			e, _ := DecodeOrchestratorEvent(fixtureEvent("review.created"), "review.created", eventFixtureID)
			out, err := (EventRepository{eventBegin{tx: tx}}).Accept(context.Background(), e)
			if !tx.rolled || tx.committed != (stage != "query") || ((err == nil) != (stage == "ok")) || (stage != "ok" && out != "") {
				t.Fatal(fmt.Sprint(stage, out, err))
			}
		})
	}
}
func TestEventDeadLetterPrivacyAndReplayIsolation(t *testing.T) {
	d, err := DecodeDeadLetter([]byte(`{"task_id":"44444444-4444-4444-8444-444444444444","token":"SECRET"}`), "dead.events")
	if err != nil || d.WorkerClass != "events" || d.TaskID != nil || d.EventID != nil || strings.Contains(string(d.Metadata), "SECRET") {
		t.Fatal("event replay/privacy boundary")
	}
}

func TestLegacyTerminalTelemetryIsNotInvented(t *testing.T) {
	for _, kind := range []string{"task.failed", "task.cancelled", "task.succeeded"} {
		for _, metrics := range []string{`{}`, `{"done":1}`, `{"duration_ms":1}`} {
			var m map[string]json.RawMessage
			json.Unmarshal(fixtureEvent(kind), &m)
			m["metrics"] = json.RawMessage(metrics)
			raw, _ := json.Marshal(m)
			e, err := DecodeOrchestratorEvent(raw, kind, eventFixtureID)
			if err != nil {
				t.Fatal("legacy terminal telemetry rejected")
			}
			var after map[string]json.RawMessage
			json.Unmarshal(e.Payload, &after)
			var a, b any
			json.Unmarshal(after["metrics"], &a)
			json.Unmarshal([]byte(metrics), &b)
			if fmt.Sprint(a) != fmt.Sprint(b) {
				t.Fatal("telemetry fabricated")
			}
		}
	}
	for _, metrics := range []string{`null`, `[]`, `"x"`, `{"unknown_metric":1}`, `{"done":-1}`, `{"duration_ms":null}`} {
		var m map[string]json.RawMessage
		json.Unmarshal(fixtureEvent("task.failed"), &m)
		m["metrics"] = json.RawMessage(metrics)
		raw, _ := json.Marshal(m)
		if _, err := DecodeOrchestratorEvent(raw, "task.failed", eventFixtureID); err == nil {
			t.Fatal("invalid telemetry accepted")
		}
	}
	var m map[string]json.RawMessage
	json.Unmarshal(fixtureEvent("task.failed"), &m)
	delete(m, "metrics")
	raw, _ := json.Marshal(m)
	if _, err := DecodeOrchestratorEvent(raw, "task.failed", eventFixtureID); err == nil {
		t.Fatal("missing metrics object accepted")
	}
	json.Unmarshal(fixtureEvent("task.progress"), &m)
	m["metrics"] = json.RawMessage(`{}`)
	raw, _ = json.Marshal(m)
	if _, err := DecodeOrchestratorEvent(raw, "task.progress", eventFixtureID); err == nil {
		t.Fatal("legacy exception widened to progress")
	}
}

type eventSchemaQuery struct {
	ok    bool
	err   error
	pin   string
	query string
}

func (q *eventSchemaQuery) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.query = sql
	if len(args) == 3 {
		q.pin, _ = args[0].(string)
	}
	return eventSchemaRow{q.ok, q.err}
}

type eventSchemaRow struct {
	ok  bool
	err error
}

func (r eventSchemaRow) Scan(dest ...any) error {
	if r.err == nil {
		*(dest[0].(*bool)) = r.ok
	}
	return r.err
}
func TestEventSchemaGuardFailsClosedAndPinsAuthority(t *testing.T) {
	for _, ok := range []bool{false, true} {
		q := &eventSchemaQuery{ok: ok}
		err := RequireEventConsumerSchema(context.Background(), q)
		if (err == nil) != ok {
			t.Fatal("schema result ignored")
		}
		if q.pin != eventAcceptanceSourceSHA256 || len(q.pin) != 64 {
			t.Fatal("source pin absent")
		}
		for _, required := range []string{"p.proconfig=ARRAY", "a.is_grantable", "relforcerowsecurity", "has_table_privilege", "p.prorettype='text'", "p.prosecdef", "photo.emit_task_progress", "heartbeat_task_pre_v23_series_device", "task_progress_outbox_lookup"} {
			if !strings.Contains(q.query, required) {
				t.Fatal("missing authority guard")
			}
		}
	}
	q := &eventSchemaQuery{err: errors.New("database failure")}
	if RequireEventConsumerSchema(context.Background(), q) == nil {
		t.Fatal("schema query failure accepted")
	}
}

func TestEventReconnectBackoffAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	delays := []time.Duration{}
	runEventReconnect(ctx, func(context.Context) error { calls++; return errors.New("unavailable") }, func(_ context.Context, d time.Duration) bool {
		delays = append(delays, d)
		if len(delays) == 7 {
			cancel()
			return false
		}
		return true
	})
	expected := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 15 * time.Second, 15 * time.Second}
	if calls != len(expected) || fmt.Sprint(delays) != fmt.Sprint(expected) {
		t.Fatal("reconnect lacks bounded backoff")
	}
	cancelled, c := context.WithCancel(context.Background())
	c()
	if eventReconnectWait(cancelled, time.Hour) {
		t.Fatal("cancelled backoff continued")
	}
	calls = 0
	runEventReconnect(cancelled, func(context.Context) error { calls++; return nil }, eventReconnectWait)
	if calls != 0 {
		t.Fatal("session created after shutdown")
	}
}

func TestLegacyTerminalErrorFieldsAreOptionalNotInvented(t *testing.T) {
	for _, kind := range []string{"task.failed", "task.cancelled"} {
		var m map[string]json.RawMessage
		json.Unmarshal(fixtureEvent(kind), &m)
		m["error"] = json.RawMessage(`{"code":"LEASE_LOST","retryable":true}`)
		raw, _ := json.Marshal(m)
		e, err := DecodeOrchestratorEvent(raw, kind, eventFixtureID)
		if err != nil {
			t.Fatal("minimal installed SQL error rejected")
		}
		var after map[string]json.RawMessage
		json.Unmarshal(e.Payload, &after)
		var detail map[string]json.RawMessage
		json.Unmarshal(after["error"], &detail)
		if len(detail) != 2 {
			t.Fatal("legacy error defaults invented")
		}
	}
	for _, detail := range []string{`{}`, `{"code":"X"}`, `{"code":"X","retryable":null}`, `{"code":"X","retryable":true,"message_key":null}`, `{"code":"X","retryable":true,"args":[]}`, `{"code":"X","retryable":true,"args":{"nested":{}}}`, `{"code":"X","retryable":true,"unknown":"secret"}`} {
		var m map[string]json.RawMessage
		json.Unmarshal(fixtureEvent("task.failed"), &m)
		m["error"] = json.RawMessage(detail)
		raw, _ := json.Marshal(m)
		if _, err := DecodeOrchestratorEvent(raw, "task.failed", eventFixtureID); err == nil {
			t.Fatal("invalid error accepted")
		}
	}
}

func TestEventDLQPoisonBoundariesPersistThenAck(t *testing.T) {
	for _, body := range [][]byte{nil, {}, []byte("secret-marker"), []byte(strings.Repeat("SECRET", 12000))} {
		ack := &eventAck{}
		persisted := false
		err := ProcessDeadLetterDelivery(context.Background(), amqp.Delivery{Acknowledger: ack, RoutingKey: "dead.events", Body: body}, func(_ context.Context, r DeadLetterRecord) error {
			persisted = true
			if r.WorkerClass != "events" || r.TaskID != nil || r.EventID != nil || len(r.PayloadSHA256) != 64 || strings.Contains(string(r.Metadata), "SECRET") {
				t.Fatal("event quarantine leak")
			}
			return nil
		})
		if err != nil || !persisted || !ack.acked || ack.rejected {
			t.Fatal("event poison not durably ACKed")
		}
	}
}
func TestEventConsumerExplicitConfiguration(t *testing.T) {
	dsn := "postgres://worker:password@127.0.0.1:57432/photoeditor?sslmode=disable"
	broker := "amqp://worker:password@127.0.0.1:56672/project"
	for _, pair := range [][2]string{{"", broker}, {"  ", broker}, {dsn, ""}, {dsn, "  "}, {"dbname=photoeditor", broker}, {"postgres:///db", broker}, {dsn, "amqp://"}, {dsn, "amqp://localhost"}} {
		if validateEventConsumerConfig(pair[0], pair[1]) == nil {
			t.Fatal("ambient configuration allowed")
		}
	}
	for _, database := range []string{dsn, "host=127.0.0.1 port=57432 user=worker dbname=photoeditor password='not printed' sslmode=disable"} {
		if validateEventConsumerConfig(database, broker) != nil {
			t.Fatal("explicit configuration rejected")
		}
	}
}
func TestDeliveryCancelledProducerContractAndBinding(t *testing.T) {
	raw := []byte(`{"schema_version":1,"event_id":"11111111-1111-4111-8111-111111111111","tenant_id":"22222222-2222-4222-8222-222222222222","run_id":"33333333-3333-4333-8333-333333333333","type":"delivery.cancelled","package_id":"44444444-4444-4444-8444-444444444444","status":"cancelled"}`)
	if _, err := DecodeOrchestratorEvent(raw, "delivery.cancelled", eventFixtureID); err != nil {
		t.Fatal("actual delivery producer shape rejected")
	}
	for _, bad := range []string{strings.Replace(string(raw), `"status":"cancelled"`, `"status":"ready"`, 1), strings.Replace(string(raw), `"package_id"`, `"download_job_id"`, 1)} {
		if _, err := DecodeOrchestratorEvent([]byte(bad), "delivery.cancelled", eventFixtureID); err == nil {
			t.Fatal("invalid delivery event accepted")
		}
	}
	bound := false
	for _, b := range Bindings {
		if b.Queue == "q.orchestrator.events" && b.Exchange == Events && b.Key == "delivery.cancelled" {
			bound = true
		}
	}
	if !bound {
		t.Fatal("mandatory delivery publisher unroutable")
	}
}

type eventDLQFailureAck struct {
	eventAck
	nacked  bool
	requeue bool
}

func (a *eventDLQFailureAck) Nack(_ uint64, multiple, requeue bool) error {
	if multiple {
		panic("multiple NACK")
	}
	a.nacked = true
	a.requeue = requeue
	return nil
}
func TestEventDLQPersistenceFailureIsNotAcked(t *testing.T) {
	a := &eventDLQFailureAck{}
	called := false
	err := ProcessDeadLetterDelivery(context.Background(), amqp.Delivery{Acknowledger: a, RoutingKey: "dead.events", Body: nil}, func(context.Context, DeadLetterRecord) error {
		called = true
		return errors.New("database unavailable")
	})
	if err != nil || !called || a.acked || !a.nacked || !a.requeue {
		t.Fatal("unpersisted event diagnostic ACKed")
	}
}
func TestEventDLQAckLossPropagates(t *testing.T) {
	a := &eventAck{err: errors.New("ack lost")}
	persisted := false
	err := ProcessDeadLetterDelivery(context.Background(), amqp.Delivery{Acknowledger: a, RoutingKey: "dead.events", Body: nil}, func(context.Context, DeadLetterRecord) error { persisted = true; return nil })
	if err == nil || !persisted || !a.acked {
		t.Fatal("ACK-loss result hidden")
	}
}
