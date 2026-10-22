package contactsheet

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

// Accepted requests survive selection expiry and unavailable font storage.
// This fixture requires an isolated database; it is authored for the test phase.
func TestIntegrationContactSheetExpiredSelectionReplay(t *testing.T) {
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
	applicationName:="contact-sheet-receipt-"+uuid.NewString()
	cfg.ConnConfig.RuntimeParams["application_name"]=applicationName
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	var actor, tenantID, projectID, collectionID, snapshotID, runID, authSession uuid.UUID
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
	if err = admin.QueryRow(ctx, `INSERT INTO photo.snapshots
	 (tenant_id,kind,collection_id,source_revision,principal_type,principal_id,
	 digest,item_count,expires_at) VALUES($1,'selection',$2,1,'user',$3,$4,1,
	 now()-interval '1 hour') RETURNING id`, tenantID, collectionID, actor,
		strings.Repeat("a", 64)).Scan(&snapshotID); err != nil { t.Fatal(err) }
	if err = admin.QueryRow(ctx, `INSERT INTO photo.runs
	 (tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,
	 created_by,failure_policy) VALUES($1,'export','queued',$2,jsonb_build_object('schema_version',1,'output_kind','contact_sheet','selection_snapshot_id',$2::text,'locale','en','columns',4,'rows',5),$3,$4,'fail_fast')
	 RETURNING id`, tenantID, snapshotID, strings.Repeat("b", 64), actor).
		Scan(&runID); err != nil { t.Fatal(err) }
	request := CreateRequest{SelectionSnapshotID: snapshotID, Locale: "en", Columns: 4, Rows: 5}
	hash, err := canonical.Hash(request)
	if err != nil { t.Fatal(err) }
	service := Service{DB: api}
	who := auth.Identity{UserID: actor, SessionID: authSession, Epoch: epoch}
	closedKey:=uuid.NewString()
	// Exercise an old unsealed short-lived receipt: Use's expiry argument alone
	// must not be mistaken for an update of an existing receipt.
	if _,err=admin.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,'ContactSheet.Create',$3,$4,now()-interval '1 hour')`,tenantID,actor,closedKey,hash);err!=nil{t.Fatal(err)}
	closed,err:=service.Abandon(ctx,who,tenantID,closedKey,request)
	if err!=nil||!isAbandoned(closed,request,closedKey){t.Fatalf("abandon without resources: %+v %v",closed,err)}
	var permanent bool
	if err=admin.QueryRow(ctx,`SELECT parent_binding=$4 AND expires_at>='9999-01-01'::timestamptz FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='ContactSheet.Create' AND key=$3`,tenantID,actor,closedKey,"selection:"+snapshotID.String()).Scan(&permanent);err!=nil||!permanent{t.Fatalf("tombstone retention: %v %v",permanent,err)}
	_,err=service.Create(ctx,who,tenantID,closedKey,request)
	var abandoned *domain.Error
	if !errors.As(err,&abandoned)||abandoned.Code!="CONTACT_SHEET_ABANDONED"{t.Fatalf("late create bypassed tombstone: %v",err)}
	key := uuid.NewString()
	body, err := json.Marshal(Accepted{RunID: runID, Status: "queued"})
	if err != nil { t.Fatal(err) }
	if _, err = admin.Exec(ctx, `INSERT INTO photo.idempotency_records
	 (tenant_id,principal_type,principal_id,route,key,request_hash,response_status,
	 response_body,resource_id,expires_at) VALUES($1,'user',$2,'ContactSheet.Create',
	 $3,$4,202,$5,$6,now()+interval '1 day')`, tenantID, actor, key, hash, body, runID);
		err != nil { t.Fatal(err) }
	// No ResourceManager/ObjectStore configured: replay must bypass them.
	response, err := service.Create(ctx, who, tenantID, key, request)
	if err != nil || !response.Replayed || response.Status != 202 ||
		response.ResourceID == nil || *response.ResourceID != runID {
		t.Fatalf("expired snapshot receipt: %+v, %v", response, err)
	}
	var ticket Accepted
	if err = json.Unmarshal(response.Body, &ticket); err != nil || ticket.RunID != runID {
		t.Fatalf("receipt body: %+v, %v", ticket, err)
	}
	resolved,err:=service.Abandon(ctx,who,tenantID,key,request)
	if err!=nil||resolved.Status!=202||resolved.ResourceID==nil||*resolved.ResourceID!=runID||string(resolved.Body)!=string(response.Body){t.Fatalf("accepted receipt must win abandonment: %+v %v",resolved,err)}
	changed := request
	changed.Columns = 3
	if _, err = service.Create(ctx, who, tenantID, key, changed); err == nil {
		t.Fatal("same key accepted a changed request")
	}
	if _, err = service.Create(ctx, who, tenantID, uuid.NewString(), request); err == nil {
		t.Fatal("expired selection accepted as a fresh request")
	}
 // Non-owner Admin loses its restricted-project bypass during the actual
 // selection-table wait. Fresh project authority must use the post-wait role.
 var delegated,delegatedSession,delegatedSnapshot uuid.UUID;var delegatedEpoch int64
 if err=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&delegated,&delegatedEpoch);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'admin','active')`,tenantID,delegated);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,'native',now()+interval '1 hour') RETURNING id`,delegated,auth.HashSecret(uuid.NewString()),uuid.New()).Scan(&delegatedSession);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.projects SET visibility='restricted' WHERE tenant_id=$1 AND id=$2`,tenantID,projectID);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count) VALUES($1,'selection',$2,1,'user',$3,$4,0) RETURNING id`,tenantID,collectionID,delegated,strings.Repeat("c",64)).Scan(&delegatedSnapshot);err!=nil{t.Fatal(err)}
 gate,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer gate.Rollback(context.WithoutCancel(ctx));var gatePID int
 if err=gate.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&gatePID);err!=nil{t.Fatal(err)};if _,err=gate.Exec(ctx,`LOCK TABLE photo.snapshots IN ACCESS EXCLUSIVE MODE`);err!=nil{t.Fatal(err)}
 refused:=make(chan error,1);delegatedKey:=uuid.NewString();go func(){_,e:=service.Abandon(ctx,auth.Identity{UserID:delegated,SessionID:delegatedSession,Epoch:delegatedEpoch},tenantID,delegatedKey,CreateRequest{SelectionSnapshotID:delegatedSnapshot,Locale:"en",Columns:4,Rows:5});refused<-e}()
 boundary:=time.NewTimer(3*time.Second);defer boundary.Stop();poll:=time.NewTicker(10*time.Millisecond);defer poll.Stop();waiting:=false
 for !waiting{select{case e:=<-refused:t.Fatal("selection did not wait",e);case <-boundary.C:t.Fatal("selection wait not observed");case <-poll.C:if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,gatePID).Scan(&waiting);err!=nil{t.Fatal(err)}}}
 if _,err=admin.Exec(ctx,`UPDATE photo.memberships SET role='editor' WHERE tenant_id=$1 AND user_id=$2`,tenantID,delegated);err!=nil{t.Fatal(err)};if err=gate.Commit(ctx);err!=nil{t.Fatal(err)}
 select{case e:=<-refused:if e==nil{t.Fatal("stale Admin authorized restricted selection")};case <-ctx.Done():t.Fatal(ctx.Err())}
 var receipts int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='ContactSheet.Create' AND key=$3`,tenantID,delegated,delegatedKey).Scan(&receipts);err!=nil||receipts!=0{t.Fatal("withdrawn authority wrote tombstone",receipts,err)}
	// Observe an actual second-connection receipt wait, then revoke project
	// visibility. Preflight authority must not survive that wait as replay proof.
	locked,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)}
	defer locked.Rollback(context.Background())
	var one int
	if err=locked.QueryRow(ctx,`SELECT 1 FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='ContactSheet.Create' AND key=$3 FOR UPDATE`,tenantID,actor,key).Scan(&one);err!=nil{t.Fatal(err)}
	finished:=make(chan error,1)
	go func(){_,e:=service.Abandon(ctx,who,tenantID,key,request);finished<-e}()
	ticker:=time.NewTicker(10*time.Millisecond);defer ticker.Stop()
	waitTimeout:=time.NewTimer(3*time.Second);defer waitTimeout.Stop()
	for waiting:=false;!waiting;{
	 if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND query LIKE '%idempotency_records%')`,applicationName).Scan(&waiting);err!=nil{t.Fatal(err)}
	 if waiting{break};select{case e:=<-finished:t.Fatalf("abandon did not wait for receipt: %v",e);case <-waitTimeout.C:t.Fatal("receipt wait was not observed");case <-ticker.C:}
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.projects SET archived_at=now()
	 WHERE tenant_id=$1 AND id=$2`, tenantID, projectID); err != nil { t.Fatal(err) }
	if err=locked.Commit(ctx);err!=nil{t.Fatal(err)}
	select{case e:=<-finished:if e==nil{t.Fatal("accepted receipt exposed after project revoked during lock wait")};case <-ctx.Done():t.Fatal("abandon did not finish after receipt release")}
	if _, err = service.Create(ctx, who, tenantID, key, request); err == nil {
		t.Fatal("receipt exposed after project archival")
	}
}
