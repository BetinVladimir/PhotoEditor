package queue

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Dedicated empty recovery DB/vhost only. This test never drains a shared queue.
func TestIntegrationRestoreTaskReplayConfirmedIdentityAndRetry(t *testing.T) {
	if os.Getenv("PHOTO_TEST_RESTORE_REPLAY") != "1" {
		t.Skip("dedicated empty restore DB and isolated vhost required")
	}
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var database string
	var pending int64
	if err = admin.QueryRow(ctx, "SELECT current_database(),count(*) FROM photo.outbox WHERE sent_at IS NULL").Scan(&database, &pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatal("refusing nonempty shared outbox")
	}
	conn, ch := broker(t)
	route := "restore.integration." + uuid.NewString()
	queue, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = ch.QueueBind(queue.Name, route, Events, false, nil); err != nil {
		t.Fatal(err)
	}
	publisher, err := NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_publisher"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var user, tenant uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@example.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	rows, err := admin.Query(ctx, `INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 SELECT $1,gen_random_uuid(),$2,$3,jsonb_build_object('schema_version',1,'probe','restore-replay')
 FROM generate_series(1,205) RETURNING id,event_id`, tenant, Events, route)
	if err != nil {
		t.Fatal(err)
	}
	ids := []uuid.UUID{}
	expected := map[string]bool{}
	for rows.Next() {
		var id, event uuid.UUID
		if err = rows.Scan(&id, &event); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		ids = append(ids, id)
		expected[event.String()] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupDB, err := pgxpool.New(cleanup, dsn)
		if err != nil {
			t.Errorf("cleanup connection: %v", err)
			return
		}
		defer cleanupDB.Close()
		if _, err = cleanupDB.Exec(cleanup, "DELETE FROM photo.outbox WHERE id=ANY($1)", ids); err != nil {
			t.Errorf("cleanup outbox: %v", err)
		}
	})
	guardCalls := 0
	guard := func(ctx context.Context) error {
		guardCalls++
		if conn.IsClosed() {
			return errors.New("isolated broker lost")
		}
		var actual string
		if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&actual); err != nil {
			return err
		}
		if actual != database {
			return errors.New("database changed")
		}
		return ctx.Err()
	}

	// Build an independent catalogue from privileged SQL, then verify the
	// publisher-only bounded pages against it before any broker publication.
	inventory := newRestoreEnvelopeInventory()
	catalogue, err := admin.Query(ctx, "SELECT id,tenant_id,event_id,destination,routing_key,payload FROM photo.outbox WHERE sent_at IS NULL ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for catalogue.Next() {
		var id, scope, event uuid.UUID
		var destination, routing string
		var payload []byte
		if err = catalogue.Scan(&id, &scope, &event, &destination, &routing, &payload); err == nil {
			err = inventory.append(id, scope, event, destination, routing, payload)
		}
		if err != nil {
			catalogue.Close()
			t.Fatal(err)
		}
	}
	catalogue.Close()
	if err = catalogue.Err(); err != nil {
		t.Fatal(err)
	}
	inventorySHA := hex.EncodeToString(inventory.digest.Sum(nil))
	inventoryOutbox := Outbox{DB: pool, Publisher: publisher}
	if err = inventoryOutbox.verifyRestoreInventory(ctx, database, 205, inventorySHA, guard); err != nil {
		t.Fatal(err)
	}
	// Keep the count and the independently computed digest valid while making
	// one row impossible for claim_outbox_batch('task') to select.
	if _, err = admin.Exec(ctx, "UPDATE photo.outbox SET destination='photo.unclaimable.v1' WHERE id=$1", ids[0]); err != nil {
		t.Fatal(err)
	}
	unclaimable := newRestoreEnvelopeInventory()
	badRows, err := admin.Query(ctx, "SELECT id,tenant_id,event_id,destination,routing_key,payload FROM photo.outbox WHERE sent_at IS NULL ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for badRows.Next() {
		var id, scope, event uuid.UUID
		var destination, routing string
		var payload []byte
		if err = badRows.Scan(&id, &scope, &event, &destination, &routing, &payload); err == nil {
			err = unclaimable.append(id, scope, event, destination, routing, payload)
		}
		if err != nil {
			badRows.Close()
			t.Fatal(err)
		}
	}
	badRows.Close()
	if err = badRows.Err(); err != nil || unclaimable.count != inventory.count {
		t.Fatal("unclaimable catalogue invalid", err)
	}
	badSHA := hex.EncodeToString(unclaimable.digest.Sum(nil))
	if result, refusal := inventoryOutbox.replayRestoredTasks(ctx, database, guard, 300, &unclaimable.count, badSHA); refusal == nil || !result.CompletedAt.IsZero() {
		t.Fatal("unclaimable destination passed replay preflight")
	}
	if _, ok, getErr := ch.Get(queue.Name, true); getErr != nil || ok {
		t.Fatal("unclaimable destination allowed partial broker publication")
	}
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.outbox WHERE id=ANY($1) AND (sent_at IS NOT NULL OR publish_lease_until IS NOT NULL)", ids).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("unclaimable destination preflight claimed events", err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.outbox SET destination=$2 WHERE id=$1", ids[0], Events); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.outbox SET payload=jsonb_build_object('changed',true) WHERE id=$1", ids[0]); err != nil {
		t.Fatal(err)
	}
	if result, refusal := inventoryOutbox.replayRestoredTasks(ctx, database, guard, 300, &inventory.count, inventorySHA); refusal == nil || !result.CompletedAt.IsZero() {
		t.Fatal("same-count substitution accepted")
	}
	if _, ok, getErr := ch.Get(queue.Name, true); getErr != nil || ok {
		t.Fatal("substituted inventory published")
	}
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.outbox WHERE id=ANY($1) AND (sent_at IS NOT NULL OR publish_lease_until IS NOT NULL)", ids).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("inventory refusal claimed events", err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.outbox SET payload=jsonb_build_object('schema_version',1,'probe','restore-replay') WHERE id=$1", ids[0]); err != nil {
		t.Fatal(err)
	}
	unknown := Outbox{DB: pool, Publisher: failedPublisher{}}
	if result, err := unknown.ReplayRestoredTasks(ctx, database, guard, 300); err == nil || !result.CompletedAt.IsZero() {
		t.Fatal("unknown broker outcome granted replay")
	}
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.outbox WHERE id=ANY($1) AND sent_at IS NOT NULL", ids).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("unknown publish lost events", err)
	}
	// Simulates operator claim expiry after an interrupted recovery. Original
	// event IDs are never replaced and no sent record is reset.
	if _, err = admin.Exec(ctx, "UPDATE photo.outbox SET publish_lease_until=NULL WHERE id=ANY($1)", ids); err != nil {
		t.Fatal(err)
	}
	outbox := Outbox{DB: pool, Publisher: publisher}
	var sentSince time.Time
	if err = admin.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&sentSince); err != nil {
		t.Fatal(err)
	}
	var publisherAllowed, apiAllowed bool
	if err = admin.QueryRow(ctx, "SELECT has_function_privilege('photo_publisher','photo.restore_task_outbox_sent_page(text,timestamptz,uuid,integer)','EXECUTE'),has_function_privilege('photo_api','photo.restore_task_outbox_sent_page(text,timestamptz,uuid,integer)','EXECUTE')").Scan(&publisherAllowed, &apiAllowed); err != nil || !publisherAllowed || apiAllowed {
		t.Fatal("restore sent inventory ACL invalid", err)
	}
	var sentPageCount int64
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM photo.restore_task_outbox_sent_page($1,$2,NULL,1)", database, sentSince.Add(-21*time.Minute)).Scan(&sentPageCount); err == nil {
		t.Fatal("unbounded sent inventory history accepted")
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM photo.restore_task_outbox_sent_page($1,$2,NULL,1)", database+"_foreign", sentSince).Scan(&sentPageCount); err == nil {
		t.Fatal("foreign sent inventory database accepted")
	}
	result, err := outbox.replayRestoredTasks(ctx, database, guard, 300, &inventory.count, inventorySHA)
	if err != nil || result.Confirmed != 205 || result.InitiallyPending != 205 || guardCalls < 3 {
		t.Fatalf("replay evidence %#v %v", result, err)
	}
	for range ids {
		delivery, ok, err := ch.Get(queue.Name, false)
		if err != nil || !ok {
			t.Fatal("confirmed event missing", err)
		}
		if !expected[delivery.MessageId] || !json.Valid(delivery.Body) {
			t.Fatal("event identity changed or duplicated")
		}
		delete(expected, delivery.MessageId)
		if err = delivery.Ack(false); err != nil {
			t.Fatal(err)
		}
	}
	if len(expected) != 0 {
		t.Fatal("events missing")
	}
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.outbox WHERE id=ANY($1) AND sent_at IS NOT NULL", ids).Scan(&pending); err != nil || pending != 205 {
		t.Fatal("confirmation not persisted", err)
	}
	if err = outbox.verifyRestoreSentInventory(ctx, database, sentSince, 205, inventorySHA, guard); err != nil {
		t.Fatal("sent identity not retained", err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.outbox SET payload=jsonb_build_object('changed',true) WHERE id=$1", ids[0]); err != nil {
		t.Fatal(err)
	}
	if err = outbox.verifyRestoreSentInventory(ctx, database, sentSince, 205, inventorySHA, guard); err == nil {
		t.Fatal("same-count sent substitution accepted")
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.outbox SET payload=jsonb_build_object('schema_version',1,'probe','restore-replay') WHERE id=$1", ids[0]); err != nil {
		t.Fatal(err)
	}
	again, err := outbox.ReplayRestoredTasks(ctx, database, guard, 300)
	if err != nil || again.Confirmed != 0 {
		t.Fatal("sent rows replayed", err)
	}
	if _, ok, err := ch.Get(queue.Name, true); err != nil || ok {
		t.Fatal("unexpected duplicate after completed replay", err)
	}
}
