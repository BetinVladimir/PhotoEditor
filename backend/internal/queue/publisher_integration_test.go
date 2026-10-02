package queue

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"os"
	"testing"
	"time"
)

func broker(t *testing.T) (*amqp.Connection, *amqp.Channel) {
	t.Helper()
	url := os.Getenv("PHOTO_TEST_AMQP_URL")
	if url == "" {
		t.Skip("isolated Rabbit vhost required")
	}
	conn, err := amqp.DialConfig(url, amqp.Config{Heartbeat: 30 * time.Second})
	if err != nil {
		t.Fatal("broker connection failed")
	}
	t.Cleanup(func() { conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ch.Close() })
	if err = Declare(ch); err != nil {
		t.Fatal(err)
	}
	return conn, ch
}
func TestIntegrationMandatoryConfirm(t *testing.T) {
	conn, ch := broker(t)
	p, err := NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := uuid.New()
	e := Envelope{EventID: id, Destination: Commands, RoutingKey: "unbound.key", Payload: json.RawMessage(`{"schema_version":1}`)}
	if err = p.Publish(ctx, e); err == nil {
		t.Fatal("unroutable message marked sent")
	}
	e.RoutingKey = "io.verify"
	if err = p.Publish(ctx, e); err != nil {
		t.Fatal(err)
	}
	delivery, ok, err := ch.Get("q.worker.media", false)
	if err != nil || !ok {
		t.Fatal("message missing", err)
	}
	if delivery.MessageId != id.String() || delivery.DeliveryMode != amqp.Persistent {
		t.Fatal("message properties")
	}
	if err = delivery.Ack(false); err != nil {
		t.Fatal(err)
	}
}

type failedPublisher struct{}

func (failedPublisher) Publish(context.Context, Envelope) error {
	return errors.New("simulated unknown confirmation")
}
func TestIntegrationOutboxUnknownOutcome(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DB required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, _ := pgxpool.ParseConfig(dsn)
	cfg.ConnConfig.RuntimeParams["role"] = "photo_publisher"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var user uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@example.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	var tid uuid.UUID
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tid); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	event := uuid.New()
	_, err = admin.Exec(ctx, `INSERT INTO photo.outbox(id,tenant_id,event_id,destination,routing_key,payload) VALUES($1,$2,$3,$4,'io.verify','{}')`, id, tid, event, Commands)
	if err != nil {
		t.Fatal(err)
	}
	outbox := Outbox{DB: pool, Publisher: failedPublisher{}}
	sent, err := outbox.Dispatch(ctx, "task", 1)
	if err == nil || sent != 0 {
		t.Fatal("unknown publish marked sent")
	}
	var sentAt *time.Time
	if err = admin.QueryRow(ctx, "SELECT sent_at FROM photo.outbox WHERE id=$1", id).Scan(&sentAt); err != nil || sentAt != nil {
		t.Fatal("outbox lost", err)
	}
	_, err = admin.Exec(ctx, "UPDATE photo.outbox SET publish_lease_until=now()-interval '1 second' WHERE id=$1", id)
	if err != nil {
		t.Fatal(err)
	}
	var reclaimed uuid.UUID
	err = pool.QueryRow(ctx, "SELECT event_id FROM photo.claim_outbox_batch('task',1,60)").Scan(&reclaimed)
	if err != nil || reclaimed != event {
		t.Fatal("event ID changed on retry", err)
	}
}
