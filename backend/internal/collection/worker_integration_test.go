package collection

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"photoeditor/backend/internal/queue"
	"photoeditor/backend/internal/auth"
)

func TestIntegrationRabbitBulkMutation(t *testing.T) {
	dsn, brokerURL := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_TEST_AMQP_URL")
	if dsn == "" || brokerURL == "" {
		t.Skip("isolated PostgreSQL and RabbitMQ required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	var user, tenant, project, artifact, asset, version, collection, item, snapshot, run, bulk, authSession uuid.UUID
	var authEpoch int64
	email := uuid.NewString() + "@example.test"
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", email).Scan(&user, &authEpoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.sessions
 (user_id,refresh_hash,family_id,client_type,expires_at)
 VALUES($1,$2,$3,'web',now()+interval '1 day') RETURNING id`,
 user, auth.HashSecret(uuid.NewString()), uuid.New()).Scan(&authSession); err != nil { t.Fatal(err) }
	digest := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	queries := []struct {
		query string
		args  []any
		out   *uuid.UUID
	}{
		{"SELECT id FROM photo.tenants WHERE owner_user_id=$1", []any{user}, &tenant},
		{"INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Rabbit bulk','workspace') RETURNING id", nil, &project},
		{"INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES($1,'photo',$2,'v1',$3,0,'image/jpeg','working_image') RETURNING id", []any{uuid.NewString(), digest}, &artifact},
		{"INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id", nil, &asset},
		{"INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id", []any{digest}, &version},
		{"INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES($1,$2,'Rabbit source','draft') RETURNING id", nil, &collection},
		{"INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position) VALUES($1,$2,$3,$4,0) RETURNING id", nil, &item},
	}
	for index, entry := range queries {
		var args []any
		switch index {
		case 0:
			args = entry.args
		case 1:
			args = []any{tenant}
		case 2:
			args = []any{tenant, entry.args[0], entry.args[1]}
		case 3:
			args = []any{tenant, project, artifact}
		case 4:
			args = []any{tenant, asset, artifact, entry.args[0]}
		case 5:
			args = []any{tenant, project}
		case 6:
			args = []any{tenant, collection, asset, version}
		}
		if err = admin.QueryRow(ctx, entry.query, args...).Scan(entry.out); err != nil {
			t.Fatalf("fixture query %d: %v", index, err)
		}
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count)
		VALUES($1,'selection',$2,1,'user',$3,$4,1) RETURNING id`, tenant, collection, user, digest).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload)
		VALUES($1,$2,0,$3,$4,$5,$6,'{}')`, tenant, snapshot, item, version, artifact, digest); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed)
		VALUES($1,'bulk','queued',$2,'{}',$3,$4,false) RETURNING id`, tenant, snapshot, digest, user).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.bulk_operations(tenant_id,collection_id,snapshot_id,action,params,expected_next_revision,status,created_by,run_id)
		VALUES($1,$2,$3,'rating','{"rating":4}',1,'queued',$4,$5) RETURNING id`, tenant, collection, snapshot, user, run).Scan(&bulk); err != nil {
		t.Fatal(err)
	}
	if changed := false; admin.QueryRow(ctx, "SELECT photo.enqueue_bulk_chunk()").Scan(&changed) != nil || !changed {
		t.Fatal("bulk command was not enqueued")
	}
	var event uuid.UUID
	if err = admin.QueryRow(ctx, "SELECT pending_event_id FROM photo.bulk_operations WHERE id=$1", bulk).Scan(&event); err != nil {
		t.Fatal(err)
	}

	connection, err := amqp.Dial(brokerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	if err = queue.Declare(channel); err != nil {
		t.Fatal(err)
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan error, 1)
	go func() { workerDone <- RunWorker(workerCtx, dsn, brokerURL, "127.0.0.1:0") }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		inspection, inspectErr := channel.QueueInspect("q.worker.collection")
		if inspectErr == nil && inspection.Consumers > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("collection worker did not start consuming")
		}
		time.Sleep(25 * time.Millisecond)
	}
	body, _ := json.Marshal(BulkCommand{SchemaVersion: 1, EventID: event, BulkID: bulk})
	if err = channel.PublishWithContext(ctx, queue.Commands, "collection.mutate", true, false, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: event.String(), Body: body,
	}); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(8 * time.Second)
	for {
		var bulkStatus, runStatus string
		var rating int
		if err = admin.QueryRow(ctx, `SELECT b.status,r.status,i.rating FROM photo.bulk_operations b
			JOIN photo.runs r ON r.id=b.run_id JOIN photo.collection_items i ON i.id=$2 WHERE b.id=$1`, bulk, item).Scan(&bulkStatus, &runStatus, &rating); err != nil {
			t.Fatal(err)
		}
		if bulkStatus == "completed" {
			if runStatus != "completed" || rating != 4 {
				t.Fatalf("unexpected terminal state: %s %s rating=%d", bulkStatus, runStatus, rating)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("bulk command was not consumed: %s %s rating=%d", bulkStatus, runStatus, rating)
		}
		time.Sleep(25 * time.Millisecond)
	}
	// Reports bind to the immutable selection, including after live membership
	// removal. Joining current collection items would lose or mislabel results.
	apiConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil { t.Fatal(err) }
	apiConfig.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, apiConfig)
	if err != nil { t.Fatal(err) }
	defer api.Close()
	service := Service{DB:api}
	identity := auth.Identity{UserID:user, SessionID:authSession, Epoch:authEpoch}
	for phase := 0; phase < 2; phase++ {
		report, readErr := service.GetBulk(ctx, identity, tenant, bulk, 100, "")
		if readErr != nil || len(report.Results.Items) != 1 || report.Results.Items[0].ItemID != item || report.Results.Items[0].Ordinal != 0 || report.Results.Items[0].Status != "succeeded" {
			t.Fatalf("selection-bound item report phase%d: %+v %v", phase, report, readErr)
		}
		encoded, encodeErr := json.Marshal(report.Results.Items[0])
		var wire map[string]any
		if encodeErr != nil || json.Unmarshal(encoded,&wire) != nil || wire["item_id"] != item.String() { t.Fatal("item report omitted immutable item identity") }
		if phase == 0 {
			if _, err = admin.Exec(ctx, "DELETE FROM photo.collection_items WHERE tenant_id=$1 AND id=$2", tenant, item); err != nil { t.Fatal(err) }
		}
	}
	stopWorker()
	select {
	case workerErr := <-workerDone:
		if workerErr != nil {
			t.Fatal(workerErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("collection worker did not stop")
	}
	inspection, err := channel.QueueInspect("q.worker.collection")
	if err != nil || inspection.Messages != 0 {
		t.Fatalf("command not acknowledged: %+v %v", inspection, err)
	}
}
