package collection

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

// Legacy selection hashes do not include the route collection ID. A receipt
// must still be bound to that collection, including when both are accessible.
func TestIntegrationSelectionReplayCollectionBinding(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with current migrations required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	var actor, tenantID, projectID, collectionID, snapshotID, otherCollectionID, authSession uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, `SELECT user_id,auth_epoch FROM
	 photo.auth_bootstrap_user($1,'en',true)`, uuid.NewString()+"@test.example").
		Scan(&actor, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `SELECT id FROM photo.tenants
	 WHERE owner_user_id=$1`, actor).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.sessions
	 (user_id,refresh_hash,family_id,client_type,expires_at)
	 VALUES($1,$2,$3,'web',now()+interval '1 day') RETURNING id`,
		actor, auth.HashSecret(uuid.NewString()), uuid.New()).Scan(&authSession); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.projects(tenant_id,name)
	 VALUES($1,'Contact-sheet replay') RETURNING id`, tenantID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.collections
	 (tenant_id,project_id,name) VALUES($1,$2,'Source') RETURNING id`,
		tenantID, projectID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.collections
	 (tenant_id,project_id,name) VALUES($1,$2,'Other') RETURNING id`,
		tenantID, projectID).Scan(&otherCollectionID); err != nil { t.Fatal(err) }
	if err = admin.QueryRow(ctx, `INSERT INTO photo.snapshots
	 (tenant_id,kind,collection_id,source_revision,principal_type,principal_id,
	 digest,item_count,expires_at) VALUES($1,'selection',$2,1,'user',$3,$4,1,
	 now()+interval '1 day') RETURNING id`, tenantID, collectionID, actor,
		strings.Repeat("a",64)).Scan(&snapshotID); err != nil { t.Fatal(err) }
	request := SelectionRequest{CollectionRevision:"1",Scope:"all"}
	hash, err := canonical.Hash(request)
	if err != nil { t.Fatal(err) }
	body, err := json.Marshal(SnapshotRef{ID:snapshotID,Digest:strings.Repeat("a",64),ItemCount:1,ExpiresAt:time.Now().Add(time.Hour)})
	if err != nil { t.Fatal(err) }
	key := uuid.NewString()
	if _, err = admin.Exec(ctx, `INSERT INTO photo.idempotency_records
	 (tenant_id,principal_type,principal_id,route,key,request_hash,response_status,
	 response_body,resource_id,expires_at) VALUES($1,'user',$2,'Selection.Create',
	 $3,$4,201,$5,$6,now()+interval '1 day')`, tenantID, actor, key, hash, body, snapshotID);
		err != nil { t.Fatal(err) }
	service := Service{DB:api}
	who := auth.Identity{UserID:actor,SessionID:authSession,Epoch:epoch}
	response, err := service.CreateSelection(ctx,who,tenantID,collectionID,key,request)
	if err != nil || !response.Replayed || response.ResourceID == nil || *response.ResourceID != snapshotID {
		t.Fatalf("legacy same-collection replay: %+v, %v",response,err)
	}
	if _, err = service.CreateSelection(ctx,who,tenantID,otherCollectionID,key,request); err == nil {
		t.Fatal("receipt crossed collection path boundary")
	}
	// Rejection must not destroy the accepted receipt for its original scope.
	response, err = service.CreateSelection(ctx,who,tenantID,collectionID,key,request)
	if err != nil || !response.Replayed { t.Fatalf("receipt changed: %+v, %v",response,err) }
}
