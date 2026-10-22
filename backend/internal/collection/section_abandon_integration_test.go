package collection

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
)

// Uses actual Service methods, API role/RLS and idempotency SQL. Authored only;
// requires a disposable fully migrated PostgreSQL through PHOTO_TEST_DATABASE_URL.
func sectionAbandonFixture(t *testing.T)(context.Context,*pgxpool.Pool,Service,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID){
 t.Helper()
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with current migrations required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	var actor, tenantID, projectID, collectionID, otherCollectionID, authSession uuid.UUID
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

 return ctx,admin,Service{DB:api},auth.Identity{UserID:actor,SessionID:authSession,Epoch:epoch},tenantID,collectionID,otherCollectionID
}
func TestIntegrationSectionAbandonOrdersAndParentBinding(t *testing.T){
 ctx,admin,service,who,tid,collection,other:=sectionAbandonFixture(t)
 // Create wins: abandonment must return the original receipt, not remove it.
 req:=SectionCreate{ExpectedRevision:1,Name:"Ceremony",Position:0};key:=uuid.NewString()
 accepted,err:=service.CreateSection(ctx,who,tid,collection,key,req);if err!=nil{t.Fatal(err)}
 var section Section;if err=json.Unmarshal(accepted.Body,&section);err!=nil{t.Fatal(err)}
 var expiry time.Time;if err=admin.QueryRow(ctx,`SELECT expires_at FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Collection.CreateSection' AND key=$3`,tid,who.UserID,key).Scan(&expiry);err!=nil{t.Fatal(err)}
 result,err:=service.AbandonSectionCreation(ctx,who,tid,collection,key,req);if err!=nil||result.Status!=201||string(result.Body)!=string(accepted.Body)||result.ResourceID==nil||*result.ResourceID!=section.ID{t.Fatalf("create winner: %+v %v",result,err)}
 var after time.Time;if err=admin.QueryRow(ctx,`SELECT expires_at FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Collection.CreateSection' AND key=$3`,tid,who.UserID,key).Scan(&after);err!=nil||!after.Equal(expiry){t.Fatalf("accepted expiry rewritten: %v",err)}
 if _,err=service.AbandonSectionCreation(ctx,who,tid,other,key,req);err==nil{t.Fatal("accepted receipt crossed URL parent")}
 // Abandon wins despite an obsolete revision. No fresh revision prerequisite.
 abandonedKey:=uuid.NewString();abandoned,err:=service.AbandonSectionCreation(ctx,who,tid,collection,abandonedKey,req);if err!=nil||!isSectionAbandoned(abandoned,collection,abandonedKey){t.Fatalf("abandon winner: %+v %v",abandoned,err)}
 _,err=service.CreateSection(ctx,who,tid,collection,abandonedKey,req);var failure *domain.Error;if !errors.As(err,&failure)||failure.Code!="SECTION_CREATE_ABANDONED"{t.Fatalf("late create: %v",err)}
 if _,err=service.AbandonSectionCreation(ctx,who,tid,other,abandonedKey,req);err==nil{t.Fatal("tombstone crossed URL parent")}
 var count int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.sections WHERE tenant_id=$1 AND collection_id=$2`,tid,collection).Scan(&count);err!=nil||count!=1{t.Fatalf("late create inserted section: %d %v",count,err)}
 // An existing unsealed receipt retains the original hash but must receive
 // permanent expiry when the tombstone is committed.
 pendingKey:=uuid.NewString();hash,err:=canonical.Hash(req);if err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,'Collection.CreateSection',$3,$4,now()+interval '1 hour')`,tid,who.UserID,pendingKey,hash);err!=nil{t.Fatal(err)}
 pending,err:=service.AbandonSectionCreation(ctx,who,tid,collection,pendingKey,req);if err!=nil||!isSectionAbandoned(pending,collection,pendingKey){t.Fatalf("unsealed: %+v %v",pending,err)}
 var year int;if err=admin.QueryRow(ctx,`SELECT extract(year from expires_at)::int FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Collection.CreateSection' AND key=$3`,tid,who.UserID,pendingKey).Scan(&year);err!=nil||year!=9999{t.Fatalf("tombstone retention: %d %v",year,err)}
}

func TestIntegrationSectionAbandonReceiptLockWaitRechecksSession(t *testing.T){
 ctx,admin,service,who,tid,collection,_:=sectionAbandonFixture(t)
 req:=SectionCreate{ExpectedRevision:1,Name:"Blocked",Position:0};key:=uuid.NewString();hash,err:=canonical.Hash(req);if err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,'Collection.CreateSection',$3,$4,now()+interval '1 hour')`,tid,who.UserID,key,hash);err!=nil{t.Fatal(err)}
 holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(context.Background())
 var record uuid.UUID;var pid int
 if err=holder.QueryRow(ctx,`SELECT id,pg_backend_pid() FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Collection.CreateSection' AND key=$3 FOR UPDATE`,tid,who.UserID,key).Scan(&record,&pid);err!=nil{t.Fatal(err)}
 done:=make(chan error,1);go func(){_,e:=service.AbandonSectionCreation(ctx,who,tid,collection,key,req);done<-e}()
 // Observe an actual PostgreSQL lock wait, not a timing assumption. The caller
 // has passed initial authority but cannot resolve the receipt until released.
 ticker:=time.NewTicker(10*time.Millisecond);defer ticker.Stop()
 for {var waiting bool;if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,pid).Scan(&waiting);err!=nil{t.Fatal(err)};if waiting{break};select{case e:=<-done:t.Fatalf("did not wait for receipt lock: %v",e);case <-ticker.C:case <-ctx.Done():t.Fatal(ctx.Err())}}
 if _,err=admin.Exec(ctx,`UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`,who.SessionID);err!=nil{t.Fatal(err)}
 if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
 select{case err=<-done:if err==nil{t.Fatal("abandon succeeded after session withdrawal during receipt wait")};case <-ctx.Done():t.Fatal(ctx.Err())}
 var status *int;var expiry time.Time;if err=admin.QueryRow(ctx,`SELECT response_status,expires_at FROM photo.idempotency_records WHERE id=$1`,record).Scan(&status,&expiry);err!=nil{t.Fatal(err)}
 if status!=nil||expiry.Year()==9999{t.Fatal("withdrawn caller committed tombstone/retention")}
 var count int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.sections WHERE tenant_id=$1 AND collection_id=$2`,tid,collection).Scan(&count);err!=nil||count!=0{t.Fatalf("unexpected section: %d %v",count,err)}
}
