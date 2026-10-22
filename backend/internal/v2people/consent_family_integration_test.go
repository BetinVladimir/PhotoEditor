package v2people

import (
 "bytes"
 "context"
 "encoding/json"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/pagination"
 "photoeditor/backend/internal/v2resource"
)

// Authored against the real installed SQL and photo_api role; not execution evidence.
func TestIntegrationPeopleConsentReceiptCASAndRevocation(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated current migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 admin,e:=pgxpool.New(ctx,dsn);if e!=nil{t.Fatal(e)};defer admin.Close()
 cfg,e:=pgxpool.ParseConfig(dsn);if e!=nil{t.Fatal(e)};cfg.ConnConfig.RuntimeParams["role"]="photo_api"
 api,e:=pgxpool.NewWithConfig(ctx,cfg);if e!=nil{t.Fatal(e)};defer api.Close()
 var actor,tid,pid uuid.UUID;var epoch int64
 if e=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&actor,&epoch);e!=nil{t.Fatal(e)}
 if e=admin.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,actor).Scan(&tid);e!=nil{t.Fatal(e)}
 session:=uuid.New();if _,e=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,actor,auth.HashSecret(uuid.NewString()),uuid.New());e!=nil{t.Fatal(e)}
 if e=admin.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Consent scope','workspace') RETURNING id`,tid).Scan(&pid);e!=nil{t.Fatal(e)}
 who:=auth.Identity{UserID:actor,SessionID:session,Epoch:epoch};svc:=Service{DB:api,Cursors:pagination.Codec{Key:bytes.Repeat([]byte{7},32)}}
 key:=uuid.NewString();req:=CreateRequest{ProjectID:pid,Label:"Alice"}
 created,e:=svc.Create(ctx,who,tid,key,req);if e!=nil||created.Status!=201{t.Fatal("create",e)}
 person,e:=v2resource.ID(created.Body);if e!=nil{t.Fatal(e)}
 replay,e:=svc.Create(ctx,who,tid,key,req);if e!=nil||!replay.Replayed||!bytes.Equal(created.Body,replay.Body){t.Fatal("create replay",e)}
 page,e:=svc.List(ctx,who,tid,pid,"consent_grants",1,"");if e!=nil||len(page.Items)!=1||page.NextCursor==nil{t.Fatal("consent first signed page",page,e)}
 second,e:=svc.List(ctx,who,tid,pid,"consent_grants",1,*page.NextCursor);if e!=nil||len(second.Items)!=1{t.Fatal("consent second signed page",second,e)}
 var consent uuid.UUID
 if e=admin.QueryRow(ctx,`SELECT id FROM photo.consent_grants WHERE tenant_id=$1 AND project_id=$2 AND person_id=$3 AND purpose='identity_matching'`,tid,pid,person).Scan(&consent);e!=nil{t.Fatal(e)}
 rawPerson,_:=json.Marshal(person);grantKey:=uuid.NewString();grant:=ConsentRequest{ExpectedRevision:"1",Purpose:"identity_matching",PersonID:rawPerson,Action:"grant",PolicyVersion:"v2.2"}
 accepted,e:=svc.ChangeConsent(ctx,who,tid,pid,grantKey,grant);if e!=nil||accepted.Status!=200{t.Fatal("grant",e)}
 replay,e=svc.ChangeConsent(ctx,who,tid,pid,grantKey,grant);if e!=nil||!replay.Replayed||!bytes.Equal(accepted.Body,replay.Body){t.Fatal("grant replay",e)}
 if _,e=svc.ChangeConsent(ctx,who,tid,pid,uuid.NewString(),grant);domain.AsError(e).Code!="RESOURCE_CONFLICT"{t.Fatal("stale consent CAS",e)}
 revoked,e:=svc.Revoke(ctx,who,tid,person,uuid.NewString(),CASRequest{ExpectedRevision:"1"});if e!=nil||revoked.Status!=200{t.Fatal("person revoke",e)}
 var status string;var currentEpoch int64
 if e=admin.QueryRow(ctx,`SELECT status,epoch FROM photo.consent_grants WHERE tenant_id=$1 AND id=$2`,tid,consent).Scan(&status,&currentEpoch);e!=nil||status!="revoked"||currentEpoch!=3{t.Fatal("person revoke must revoke granted consent once",status,currentEpoch,e)}
 var people int;if e=admin.QueryRow(ctx,`SELECT count(*) FROM photo.project_persons WHERE tenant_id=$1 AND project_id=$2`,tid,pid).Scan(&people);e!=nil||people!=1{t.Fatal("receipt replay duplicated person",people,e)}
}
