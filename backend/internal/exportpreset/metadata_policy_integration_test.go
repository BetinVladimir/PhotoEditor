package exportpreset

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
)

// Opt-in isolated database only; fixture creates its own owner, tenant and preset.
func TestIntegrationMetadataPolicyReplayAndRevision(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
 cfg,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)};cfg.ConnConfig.RuntimeParams["role"]="photo_api"
 api,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer api.Close()
 var user,tid uuid.UUID;var epoch int64
 if err=admin.QueryRow(ctx,"SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)",uuid.NewString()+"@preset.test.example").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,"SELECT id FROM photo.tenants WHERE owner_user_id=$1",user).Scan(&tid);err!=nil{t.Fatal(err)}
 session:=uuid.New()
 if _,err=admin.Exec(ctx,"INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',$5)",session,user,auth.HashSecret(uuid.NewString()),uuid.New(),time.Now().Add(time.Hour));err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:user,SessionID:session};service:=Service{DB:api}
 raw:=[]byte(`{"format":"jpeg","icc_id":"srgb_v1","quality":92,"subsampling":"444","alpha":"matte","matte_rgb":[1,1,1],"metadata_policy":"strip","naming_template":"{original_name}_edited","size":{"long_edge":2048,"fit":"contain","allow_upscale":false},"sharpen":{"radius":1,"strength":0.2,"threshold":0.01}}`)
 var config Config;if err=json.Unmarshal(raw,&config);err!=nil{t.Fatal(err)}
 created,err:=service.Create(ctx,who,tid,uuid.NewString(),Create{Name:"Metadata integration",Config:config});if err!=nil{t.Fatal(err)}
 var preset Preset;if err=json.Unmarshal(created.Body,&preset);err!=nil{t.Fatal(err)}
 key:=uuid.NewString();change:=MetadataPolicyChange{Revision:preset.Revision,MetadataPolicy:"copyright"}
 first,err:=service.ChangeMetadataPolicy(ctx,who,tid,preset.ID,key,change);if err!=nil||first.Replayed{t.Fatal(err)}
 replay,err:=service.ChangeMetadataPolicy(ctx,who,tid,preset.ID,key,change)
 if err!=nil||!replay.Replayed||!bytes.Equal(first.Body,replay.Body){t.Fatal("replay",err)}
 if _,err=service.ChangeMetadataPolicy(ctx,who,tid,preset.ID,uuid.NewString(),change);err==nil||domain.AsError(err).Code!="REVISION_CONFLICT"{t.Fatal("stale revision",err)}
 var saved Preset;if err=json.Unmarshal(first.Body,&saved);err!=nil{t.Fatal(err)}
 if saved.Revision!="2"||saved.Config.MetadataPolicy!="copyright"||saved.Config.Size==nil||saved.Config.Size.LongEdge==nil||*saved.Config.Size.LongEdge!=2048||saved.Config.Sharpen==nil||saved.Config.Sharpen.Strength!=.2||saved.Config.Quality==nil||*saved.Config.Quality!=92{t.Fatal("processing config changed",saved)}
 var revision int64;var hash string
 if err=admin.QueryRow(ctx,"SELECT revision,config_hash FROM photo.export_presets WHERE tenant_id=$1 AND id=$2",tid,preset.ID).Scan(&revision,&hash);err!=nil||revision!=2||hash!=saved.ConfigHash{t.Fatal("persisted state",err)}
 // Two distinct intents from the same revision must have exactly one winner.
 start:=make(chan struct{});results:=make(chan error,2)
 for _,mode:=range []string{"strip","approved_exif"}{
  go func(policy string){<-start;_,failure:=service.ChangeMetadataPolicy(ctx,who,tid,preset.ID,uuid.NewString(),MetadataPolicyChange{"2",policy});results<-failure}(mode)
 }
 close(start);successes:=0
 for n:=0;n<2;n++{failure:=<-results;if failure==nil{successes++}else if domain.AsError(failure).Code!="REVISION_CONFLICT"{t.Fatal("race failure",failure)}}
 if successes!=1{t.Fatal("concurrent winners",successes)}
 if err=admin.QueryRow(ctx,"SELECT revision FROM photo.export_presets WHERE tenant_id=$1 AND id=$2",tid,preset.ID).Scan(&revision);err!=nil||revision!=3{t.Fatal("race revision",revision,err)}
 // Replay remains the original response after a later successful edit.
 replay,err=service.ChangeMetadataPolicy(ctx,who,tid,preset.ID,key,change)
 if err!=nil||!replay.Replayed||!bytes.Equal(first.Body,replay.Body){t.Fatal("historical exact replay",err)}
 if _,err=service.ChangeMetadataPolicy(ctx,who,tid,preset.ID,key,MetadataPolicyChange{"3","strip"});err==nil{t.Fatal("changed payload reused idempotency key")}
 var foreignUser,foreignTenant uuid.UUID
 if err=admin.QueryRow(ctx,"SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)",uuid.NewString()+"@preset.test.example").Scan(&foreignUser,&epoch);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,"SELECT id FROM photo.tenants WHERE owner_user_id=$1",foreignUser).Scan(&foreignTenant);err!=nil{t.Fatal(err)}
 if _,err=service.ChangeMetadataPolicy(ctx,who,foreignTenant,preset.ID,uuid.NewString(),MetadataPolicyChange{"3","copyright"});err==nil||domain.AsError(err).Code!="FORBIDDEN"{t.Fatal("foreign tenant access",err)}
 if err=admin.QueryRow(ctx,"SELECT revision FROM photo.export_presets WHERE tenant_id=$1 AND id=$2",tid,preset.ID).Scan(&revision);err!=nil||revision!=3{t.Fatal("denial changed preset",err)}

}
