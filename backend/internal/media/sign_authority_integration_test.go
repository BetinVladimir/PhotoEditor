package media

import (
 "context"
 "errors"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
)

type authoritySigner struct{ beforeReturn func(); calls int }
func(s *authoritySigner)SignRead(_ context.Context,o objectstore.Object,_ time.Duration)(objectstore.ReadURL,error){
 s.calls++;s.beforeReturn();return objectstore.ReadURL{URL:"https://objects.example/preview?versionId="+o.VersionID,ExpiresAt:time.Now().Add(time.Minute)},nil
}

// Relational fixtures under the actual API role. No image encoding/S3 quality
// proof is implied; the external signer boundary is deliberately controlled.
func TestIntegrationMediaRechecksAuthorityAndBindingAfterSigning(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("disposable fully migrated PostgreSQL required")}
 for _,scenario:=range []string{"current_viewer","acl_removed","asset_deleted","derivative_changed","cancelled"}{t.Run(scenario,func(t *testing.T){
  ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
  admin,e:=pgxpool.New(ctx,dsn);if e!=nil{t.Fatal(e)};defer admin.Close()
  cfg,e:=pgxpool.ParseConfig(dsn);if e!=nil{t.Fatal(e)};cfg.ConnConfig.RuntimeParams["role"]="photo_api"
  api,e:=pgxpool.NewWithConfig(ctx,cfg);if e!=nil{t.Fatal(e)};defer api.Close()
  exec:=func(q string,args ...any){t.Helper();if _,err:=admin.Exec(ctx,q,args...);err!=nil{t.Fatal(err)}}
  scan:=func(q string,dest any,args ...any){t.Helper();if err:=admin.QueryRow(ctx,q,args...).Scan(dest);err!=nil{t.Fatal(err)}}
  var owner,user,tenant,project,artifact,asset,version uuid.UUID;var epoch int64
  scan(`SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`,&owner,uuid.NewString()+"@media.test")
  scan(`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,&tenant,owner)
  if e=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@media.test").Scan(&user,&epoch);e!=nil{t.Fatal(e)}
  exec(`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'viewer','active')`,tenant,user)
  session:=uuid.New();exec(`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,user,auth.HashSecret(uuid.NewString()),uuid.New())
  scan(`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Media','restricted') RETURNING id`,&project,tenant)
  exec(`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')`,tenant,project,user)
  scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at,width,height) VALUES($1,'photo',$2,'v1',$3,100,'image/jpeg','working_image','ready',now(),320,200) RETURNING id`,&artifact,tenant,uuid.NewString(),strings.Repeat("a",64))
  scan(`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,&asset,tenant,project,artifact)
  scan(`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,&version,tenant,asset,artifact,strings.Repeat("a",64))
  srgb:=strings.Repeat("b",64);profile,e:=ProfileHash("thumb320",srgb);if e!=nil{t.Fatal(e)}
  exec(`INSERT INTO photo.derivatives(tenant_id,asset_version_id,variant,profile_hash,artifact_id,status) VALUES($1,$2,'thumb320',$3,$4,'ready')`,tenant,version,profile,artifact)
  signer:=&authoritySigner{beforeReturn:func(){switch scenario{
   case "acl_removed":exec(`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tenant,project,user)
   case "asset_deleted":exec(`UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`,tenant,asset)
   case "derivative_changed":exec(`UPDATE photo.derivatives SET status='queued' WHERE tenant_id=$1 AND asset_version_id=$2`,tenant,version)
   case "cancelled":cancel()
  }}}
  value,e:=(Service{DB:api,Objects:signer,Bucket:"photo",SRGBSHA256:srgb}).Sign(ctx,auth.Identity{UserID:user,SessionID:session,Epoch:epoch},tenant,asset,Request{AssetVersionID:version,Variants:[]string{"thumb320"}})
  if signer.calls!=1{t.Fatal("fixture did not reach signer",signer.calls,e)}
  if scenario=="current_viewer"{if e!=nil||len(value.Items)!=1{t.Fatal("valid current media refused",value,e)};return}
  if len(value.Items)!=0||e==nil{t.Fatal("bearer URL escaped withdrawn authority/binding",value,e)}
  if scenario=="cancelled"{if !errors.Is(e,context.Canceled){t.Fatal(e)};return}
  want:="NOT_FOUND";if scenario=="derivative_changed"{want="MEDIA_NOT_READY"};if domain.AsError(e).Code!=want{t.Fatal(want,e)}
 })}
}
