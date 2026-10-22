package asset

import (
 "context"
 "math"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// Exercises actual SearchSimilar SQL/hydration under photo_api. Projection rows
// are isolated relational fixtures, completed through finish_asset_embedding;
// this does not qualify DINO inference, object IO or the scheduler materializer.
// Authored only; requires the disposable fully migrated test database.
func TestIntegrationSimilarSearchScoresLatestDistinctAssetsWithCurrentACL(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated current migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second);defer cancel()
 admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
 runtimeDSN:=os.Getenv("PHOTO_TEST_RUNTIME_DATABASE_URL");if runtimeDSN==""{t.Fatal("isolated nonprivileged API runtime connection required")};config,err:=pgxpool.ParseConfig(runtimeDSN);if err!=nil{t.Fatal(err)}
 config.ConnConfig.RuntimeParams["role"]="photo_api";database.GuardApplicationRole(config)
 api,err:=pgxpool.NewWithConfig(ctx,config);if err!=nil{t.Fatal(err)};defer api.Close()
 var actualRole string;if err=api.QueryRow(ctx,`SELECT current_user`).Scan(&actualRole);err!=nil||actualRole!="photo_api"{t.Fatal("API role required",actualRole,err)}
 bootstrap:=func()(uuid.UUID,uuid.UUID,int64){t.Helper();var user,tid uuid.UUID;var epoch int64;if e:=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@similar.test").Scan(&user,&epoch);e!=nil{t.Fatal(e)};if e:=admin.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,user).Scan(&tid);e!=nil{t.Fatal(e)};return user,tid,epoch}
 owner,tid,_:=bootstrap();viewer,_,epoch:=bootstrap()
 if _,err=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'viewer','active')`,tid,viewer);err!=nil{t.Fatal(err)}
 session:=uuid.New();if _,err=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,viewer,auth.HashSecret(uuid.NewString()),uuid.New());err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:viewer,SessionID:session,Epoch:epoch};service:=Service{DB:api}
 project:=func(tenant uuid.UUID,name,visibility string)uuid.UUID{t.Helper();var id uuid.UUID;if e:=admin.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,$2,$3) RETURNING id`,tenant,name,visibility).Scan(&id);e!=nil{t.Fatal(e)};return id}
 openA,openB,restricted:=project(tid,"Source","workspace"),project(tid,"Other","workspace"),project(tid,"Restricted","restricted")
 artifact:=func(tenant uuid.UUID,role string)uuid.UUID{t.Helper();var id uuid.UUID;if e:=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,100,'application/json',$4,'ready',clock_timestamp()) RETURNING id`,tenant,"similar/"+uuid.NewString(),strings.Repeat("a",64),role).Scan(&id);e!=nil{t.Fatal(e)};return id}
 asset:=func(tenant,pid uuid.UUID)uuid.UUID{t.Helper();original:=artifact(tenant,"working_image");var id uuid.UUID;if e:=admin.QueryRow(ctx,`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,tenant,pid,original).Scan(&id);e!=nil{t.Fatal(e)};return id}
 version:=func(tenant,id uuid.UUID,number int)uuid.UUID{t.Helper();master:=artifact(tenant,"working_image");var v uuid.UUID;if e:=admin.QueryRow(ctx,`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,version_no) VALUES($1,$2,$3,$4,'{}','accepted',$5) RETURNING id`,tenant,id,master,strings.Repeat("a",64),number).Scan(&v);e!=nil{t.Fatal(e)};return v}
 lock,foreignLock:=strings.Repeat("1",64),strings.Repeat("2",64)
 embedding:=func(tenant,v uuid.UUID,model string,cosine float64){t.Helper();ref:=artifact(tenant,"scene_embedding");var id,token uuid.UUID;if e:=admin.QueryRow(ctx,`INSERT INTO photo.asset_embeddings(tenant_id,asset_version_id,artifact_id) VALUES($1,$2,$3) RETURNING id,token`,tenant,v,ref).Scan(&id,&token);e!=nil{t.Fatal(e)};values:=make([]float64,384);values[0]=cosine;values[1]=math.Sqrt(1-cosine*cosine);var ok bool;if e:=admin.QueryRow(ctx,`SELECT photo.finish_asset_embedding($1,$2,$3,$4::float8[],NULL)`,id,token,model,values).Scan(&ok);e!=nil||!ok{t.Fatal("normalized projection refused",e)}}
 source:=asset(tid,openA);sourceOld:=version(tid,source,1);embedding(tid,sourceOld,lock,1)
 sourceLatest:=version(tid,source,2);embedding(tid,sourceLatest,lock,1)
 historic:=asset(tid,openA);embedding(tid,version(tid,historic,1),lock,1);historicLatest:=version(tid,historic,2);embedding(tid,historicLatest,lock,.2)
 repeated:=asset(tid,openA);embedding(tid,version(tid,repeated,1),lock,.99);embedding(tid,version(tid,repeated,2),lock,.98);repeatedLatest:=version(tid,repeated,3);embedding(tid,repeatedLatest,lock,.8)
 other:=asset(tid,openA);otherVersion:=version(tid,other,1);embedding(tid,otherVersion,lock,.7)
 elsewhere:=asset(tid,openB);elsewhereVersion:=version(tid,elsewhere,1);embedding(tid,elsewhereVersion,lock,.9)
 wrongModel:=asset(tid,openA);embedding(tid,version(tid,wrongModel,1),foreignLock,1)
 private:=asset(tid,restricted);privateVersion:=version(tid,private,1);embedding(tid,privateVersion,lock,1)
 // A latest version without its own approved vector cannot reuse history.
 pending:=asset(tid,openA);embedding(tid,version(tid,pending,1),lock,1);_ = version(tid,pending,2)
 _,otherTenant,_:=bootstrap();foreignProject:=project(otherTenant,"Foreign","workspace");foreignAsset:=asset(otherTenant,foreignProject);embedding(otherTenant,version(otherTenant,foreignAsset,1),lock,1)
 search:=func(limit int,filter *uuid.UUID,min float64)SimilarPage{t.Helper();page,e:=service.SearchSimilar(ctx,who,tid,SimilarRequest{AssetVersionID:sourceOld,ProjectID:filter,Limit:limit,MinCosine:min});if e!=nil{t.Fatal(e)};return page}
 assertIDs:=func(page SimilarPage,want ...uuid.UUID){t.Helper();if len(page.Items)!=len(want){t.Fatalf("got %d candidates, want %d: %+v",len(page.Items),len(want),page)};seen:=map[uuid.UUID]bool{};for i,item:=range page.Items{if item.Asset.ID!=want[i]||seen[item.Asset.ID]||item.Asset.ID==source{t.Fatalf("wrong/duplicate/self candidate %d: %+v",i,item)};seen[item.Asset.ID]=true}}
 page:=search(2,nil,.5);assertIDs(page,elsewhere,repeated)
 if page.Items[1].Asset.CurrentVersion==nil||page.Items[1].Asset.CurrentVersion.ID!=repeatedLatest||math.Abs(page.Items[1].Cosine-.8)>1e-12{t.Fatal("score must identify the returned latest version",page.Items[1])}
 assertIDs(search(100,nil,.5),elsewhere,repeated,other)
 assertIDs(search(100,&openA,.5),repeated,other)
 low:=search(100,&openA,.1);assertIDs(low,repeated,other,historic)
 if low.Items[2].Asset.CurrentVersion==nil||low.Items[2].Asset.CurrentVersion.ID!=historicLatest||math.Abs(low.Items[2].Cosine-.2)>1e-12{t.Fatal("historical high score leaked into current DTO",low.Items[2])}
 assertIDs(search(100,&restricted,.5))
 if _,e:=service.SearchSimilar(ctx,who,tid,SimilarRequest{AssetVersionID:privateVersion,ProjectID:&openA,Limit:10});domain.AsError(e).Code!="NOT_FOUND"{t.Fatal("project filter must not bypass source ACL",e)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')`,tid,restricted,viewer);err!=nil{t.Fatal(err)}
 assertIDs(search(100,nil,.5),private,elsewhere,repeated,other);assertIDs(search(100,&restricted,.5),private)
 if _,err=admin.Exec(ctx,`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tid,restricted,viewer);err!=nil{t.Fatal(err)}
 assertIDs(search(100,nil,.5),elsewhere,repeated,other)
 noFeatures:=asset(tid,openA);unindexed:=version(tid,noFeatures,1)
 if _,e:=service.SearchSimilar(ctx,who,tid,SimilarRequest{AssetVersionID:unindexed,Limit:10});domain.AsError(e).Code!="FEATURES_NOT_READY"{t.Fatal("read-only search must not invent features",e)}
 var count int;if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.asset_embeddings WHERE tenant_id=$1 AND asset_version_id=$2`,tid,unindexed).Scan(&count);err!=nil||count!=0{t.Fatal("Viewer search must not start materialization",count,err)}
 _=owner
}
