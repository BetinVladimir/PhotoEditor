package review

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "strings"
 "strconv"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/auth"
 collections "photoeditor/backend/internal/collection"
 "photoeditor/backend/internal/pagination"
)

// Uses the real cullingFixture projection, actual current schema, API role/RLS,
// snapshot evidence and the mounted service. Requires a disposable migrated DB.
func TestIntegrationCollectionCullingCurrentMembershipAndAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 admin,e:=pgxpool.New(ctx,dsn);if e!=nil{t.Fatal(e)};defer admin.Close()
 cfg,e:=pgxpool.ParseConfig(dsn);if e!=nil{t.Fatal(e)};cfg.ConnConfig.RuntimeParams["role"]="photo_api";applicationName:="culling-race-"+uuid.NewString();cfg.ConnConfig.RuntimeParams["application_name"]=applicationName
 api,e:=pgxpool.NewWithConfig(ctx,cfg);if e!=nil{t.Fatal(e)};defer api.Close()
 var who auth.Identity;var tid,project,collection,foreign,artifact,asset,version,item,snapshot,run,review uuid.UUID
 query:=func(sql string,args []any,dest ...any){t.Helper();if e:=admin.QueryRow(ctx,sql,args...).Scan(dest...);e!=nil{t.Fatal(e)}}
 exec:=func(sql string,args ...any){t.Helper();if _,e:=admin.Exec(ctx,sql,args...);e!=nil{t.Fatal(e)}}
 query(`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,[]any{uuid.NewString()+"@culling.test"},&who.UserID,&who.Epoch)
 query(`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,[]any{who.UserID},&tid)
 query(`INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,'web',now()+interval '1 day') RETURNING id`,[]any{who.UserID,auth.HashSecret(uuid.NewString()),uuid.New()},&who.SessionID)
 query(`INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Culling projection') RETURNING id`,[]any{tid},&project)
 query(`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Current') RETURNING id`,[]any{tid,project},&collection)
 query(`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Foreign snapshot') RETURNING id`,[]any{tid,project},&foreign)
 sha:=strings.Repeat("a",64)
 query(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,100,'image/jpeg','working_image','ready',now()) RETURNING id`,[]any{tid,uuid.NewString(),sha},&artifact)
 query(`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,[]any{tid,project,artifact},&asset)
 query(`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,[]any{tid,asset,artifact,sha},&version)
 query(`INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position,moderation_state) VALUES($1,$2,$3,$4,0,'accepted') RETURNING id`,[]any{tid,collection,asset,version},&item)
 query(`INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count) VALUES($1,'selection',$2,1,'user',$3,$4,1) RETURNING id`,[]any{tid,collection,who.UserID,sha},&snapshot)
 exec(`INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload) VALUES($1,$2,0,$3,$4,$5,$6,'{}')`,tid,snapshot,item,version,artifact,sha)
 query(`INSERT INTO photo.runs(tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,'apply',$2,jsonb_build_object('input_snapshot_digest',$3::text),$3,$4) RETURNING id`,[]any{tid,snapshot,sha,who.UserID},&run)
 query(`INSERT INTO photo.reviews(tenant_id,run_id,candidate_artifact_id,candidate_hash,kind,reasons) VALUES($1,$2,$3,$4,'culling','[]') RETURNING id`,[]any{tid,run,artifact,sha},&review)
 frozen,_:=cullingFixture();frozen.Projection.Groups.ID=artifact;frozen.Projection.Proposal.ID=artifact
 frozen.Projection.Items[0].ItemID=item;frozen.Projection.Items[0].AssetID=asset;frozen.Projection.Items[0].VersionID=version
 raw,e:=json.Marshal(frozen.Projection);if e!=nil{t.Fatal(e)}
 exec(`INSERT INTO photo.apply_culling_reviews(tenant_id,run_id,review_id,snapshot_id,snapshot_digest,projection) VALUES($1,$2,$3,$4,$5,$6::jsonb)`,tid,run,review,snapshot,sha,string(raw))
 service:=Service{DB:api,Cursors:pagination.Codec{Key:[]byte(strings.Repeat("k",32))}}
 detail,e:=service.Detail(ctx,who,tid,review);if e!=nil||detail.ID!=review||detail.RunID!=run||detail.Kind!="culling"||detail.CreatedAt.IsZero(){t.Fatal("actual detail projection",detail,e)}
 reviewPage,e:=service.List(ctx,who,tid,1,"",run.String(),"pending","culling");if e!=nil||len(reviewPage.Items)!=1||reviewPage.Items[0].ID!=review||reviewPage.Items[0].RunID!=run{t.Fatal("actual filtered list projection",reviewPage,e)}
 cullingPage,e:=service.CullingItems(ctx,who,tid,review,"");if e!=nil||len(cullingPage.Items)!=1||cullingPage.Items[0].VersionID!=version||cullingPage.NextCursor!=nil{t.Fatal("actual culling page projection",cullingPage,e)}
 page,e:=service.CollectionCullingGroups(ctx,who,tid,collection,review,"");if e!=nil||len(page.Contexts)!=1||page.Contexts[0].Matched!=1||page.Contexts[0].Items[0].CollectionItemID!=item{t.Fatalf("exact real context: %v %#v",e,page)}
 if _,e=service.CollectionCullingGroups(ctx,who,tid,foreign,review,"");e==nil{t.Fatal("foreign snapshot exposed")}
 forged:=who;forged.UserID=uuid.New();if _,e=service.CollectionCullingGroups(ctx,forged,tid,collection,review,"");e==nil{t.Fatal("foreign actor exposed projection")}
 // A real version change must preserve the frozen source and count it as
 // historical, rather than substituting the new current member's version.
 var next uuid.UUID;query(`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,version_no,parent_asset_version_id) VALUES($1,$2,$3,$4,'{}','accepted',2,$5) RETURNING id`,[]any{tid,asset,artifact,sha,version},&next)
 // Exercise the actual replacement service/trigger with the accepted direct
 // child. Invalid expected version and reverse/non-child edits leave it intact.
 collectionService:=collections.Service{DB:api}
 var initialRevision int64;query(`SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`,[]any{tid,collection},&initialRevision)
 if _,e=collectionService.Publish(ctx,who,tid,collection,collections.PublicationRequest{ExpectedRevision:initialRevision,ItemIDs:[]uuid.UUID{item},Publish:true,PublishCollection:true});e!=nil{t.Fatal("publish original version",e)}
 var revision int64;query(`SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`,[]any{tid,collection},&revision)
 if _,e=collectionService.ReplaceItemVersion(ctx,who,tid,collection,item,collections.ItemVersionReplace{ExpectedRevision:strconv.FormatInt(revision,10),ExpectedVersion:uuid.New(),Version:next});e==nil{t.Fatal("forged expected version accepted")}
 replaced,e:=collectionService.ReplaceItemVersion(ctx,who,tid,collection,item,collections.ItemVersionReplace{ExpectedRevision:strconv.FormatInt(revision,10),ExpectedVersion:version,Version:next});if e!=nil||replaced.AssetVersionID!=next{t.Fatalf("accepted direct child replacement: %v",e)}
 var publishedVersion uuid.UUID;query(`SELECT asset_version_id FROM photo.publications WHERE tenant_id=$1 AND item_id=$2`,[]any{tid,item},&publishedVersion);if publishedVersion!=version{t.Fatal("replacement silently published new version")}
 var currentlyPublished bool;query(`SELECT EXISTS(SELECT 1 FROM photo.collection_items i JOIN photo.publications p ON p.tenant_id=i.tenant_id AND p.item_id=i.id AND p.asset_version_id=i.asset_version_id AND p.state='published' WHERE i.tenant_id=$1 AND i.id=$2)`,[]any{tid,item},&currentlyPublished);if currentlyPublished{t.Fatal("new current version inherited old publication")}
 _,staleError:=admin.Exec(ctx,`INSERT INTO photo.publications(tenant_id,item_id,asset_version_id,state,published_by,published_at) VALUES($1,$2,$3,'published',$4,now())`,tid,item,version,who.UserID);var pgError *pgconn.PgError;if !errors.As(staleError,&pgError)||pgError.Code!="23514"{t.Fatalf("new historical publication not rejected: %v",staleError)}
 query(`SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`,[]any{tid,collection},&revision)
 if _,e=collectionService.Publish(ctx,who,tid,collection,collections.PublicationRequest{ExpectedRevision:revision,ItemIDs:[]uuid.UUID{item},Publish:true});e!=nil{t.Fatal("explicit current-version publication",e)}
 query(`SELECT p.asset_version_id FROM photo.publications p JOIN photo.collection_items i ON i.tenant_id=p.tenant_id AND i.id=p.item_id AND i.asset_version_id=p.asset_version_id WHERE p.tenant_id=$1 AND p.item_id=$2`,[]any{tid,item},&publishedVersion);if publishedVersion!=next{t.Fatal("explicit republish retained historical version")}
 var historicalPreserved bool;query(`SELECT count(*)=2 AND count(*) FILTER(WHERE asset_version_id=$3)=1 AND count(*) FILTER(WHERE asset_version_id=$4)=1 FROM photo.publications WHERE tenant_id=$1 AND item_id=$2`,[]any{tid,item,version,next},&historicalPreserved);if !historicalPreserved{t.Fatal("explicit publication erased version history")}
 // Exercise the installed bulk publish branch through the real API-created
 // selection and bulk receipt. Only broker handoff is represented by its
 // pending-event field; no RabbitMQ or scheduler execution is claimed here.
 if e=collections.RequireCollectionPublicationHistory(ctx,api);e!=nil{t.Fatal(e)}
 exec(`DELETE FROM photo.publications WHERE tenant_id=$1 AND item_id=$2 AND asset_version_id=$3`,tid,item,next)
 query(`SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`,[]any{tid,collection},&revision)
 selected,e:=collectionService.CreateSelection(ctx,who,tid,collection,uuid.NewString(),collections.SelectionRequest{CollectionRevision:strconv.FormatInt(revision,10),Scope:"selected",ItemIDs:[]uuid.UUID{item}});if e!=nil{t.Fatal(e)}
 var bulkSnapshot collections.SnapshotRef;if e=json.Unmarshal(selected.Body,&bulkSnapshot);e!=nil{t.Fatal(e)}
 accepted,e:=collectionService.StartBulk(ctx,who,tid,collection,uuid.NewString(),collections.BulkRequest{ExpectedRevision:revision,SelectionSnapshotID:bulkSnapshot.ID,Action:"publish",Params:json.RawMessage(`{"publish":true}`)});if e!=nil{t.Fatal(e)}
 var bulkTicket collections.BulkAccepted;if e=json.Unmarshal(accepted.Body,&bulkTicket);e!=nil{t.Fatal(e)}
 event:=uuid.New();exec(`UPDATE photo.bulk_operations SET pending_event_id=$3 WHERE tenant_id=$1 AND id=$2`,tid,bulkTicket.BulkOperationID,event)
 worker,e:=admin.Begin(ctx);if e!=nil{t.Fatal(e)};defer worker.Rollback(context.WithoutCancel(ctx))
 if _,e=worker.Exec(ctx,`SET LOCAL ROLE photo_worker`);e!=nil{t.Fatal(e)};var processed bool
 if e=worker.QueryRow(ctx,`SELECT photo.process_bulk_chunk($1,$2)`,bulkTicket.BulkOperationID,event).Scan(&processed);e!=nil||!processed{t.Fatalf("installed bulk publisher: %v processed=%v",e,processed)}
 if e=worker.Commit(ctx);e!=nil{t.Fatal(e)}
 var bulkPublished bool;query(`SELECT b.status='completed' AND EXISTS(SELECT FROM photo.bulk_item_results q WHERE q.tenant_id=b.tenant_id AND q.bulk_operation_id=b.id AND q.status='succeeded') AND (SELECT count(*)=2 AND count(*) FILTER(WHERE asset_version_id=$4 AND state='published')=1 FROM photo.publications p WHERE p.tenant_id=b.tenant_id AND p.item_id=$3) FROM photo.bulk_operations b WHERE b.tenant_id=$1 AND b.id=$2`,[]any{tid,bulkTicket.BulkOperationID,item,next},&bulkPublished);if !bulkPublished{t.Fatal("bulk publication lost history or failed new current version")}
 changed,e:=service.CollectionCullingGroups(ctx,who,tid,collection,review,"");if e!=nil||len(changed.Contexts)!=1||changed.Contexts[0].Matched!=0||changed.Contexts[0].Unmatched!=1{t.Fatalf("replacement must be historical: %v %#v",e,changed)}
 query(`SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`,[]any{tid,collection},&revision)
 if _,e=collectionService.ReplaceItemVersion(ctx,who,tid,collection,item,collections.ItemVersionReplace{ExpectedRevision:strconv.FormatInt(revision,10),ExpectedVersion:next,Version:version});e==nil{t.Fatal("non-child reverse replacement accepted")}
 // Real guest item receipts and real bulk media-retry events outlive
 // membership removal; their replay never restores live download authority.
 var preset uuid.UUID;query(`INSERT INTO photo.export_presets(tenant_id,name,config,config_hash) VALUES($1,'History fixture','{}',$2) RETURNING id`,[]any{tid,sha},&preset)
 exec(`INSERT INTO photo.exports(tenant_id,asset_version_id,preset_hash,artifact_id,status,metadata_policy) VALUES($1,$2,$3,$4,'ready','{}')`,tid,next,sha,artifact)
 collectionService.Pepper=[]byte(strings.Repeat("p",32))
 share,e:=collectionService.CreateShare(ctx,who,tid,collection,uuid.NewString(),collections.ShareCreate{Label:"Historical item download",Permissions:collections.SharePermissions{View:true,Download:true},Limits:collections.ShareLimits{MaxFileBytes:1,MaxUploadBytesTotal:1,AllowedFormats:[]string{}},Moderation:"owner_review",AllowedPresets:[]uuid.UUID{preset}});if e!=nil{t.Fatal(e)}
 policyVersion,policyError:=strconv.ParseInt(share.PolicyVersion,10,64);if policyError!=nil{t.Fatal(policyError)}
 sessionHash,csrfHash:=[]byte(strings.ReplaceAll(uuid.NewString(),"-","")),[]byte(strings.ReplaceAll(uuid.NewString(),"-",""));var guestSession uuid.UUID
 query(`INSERT INTO photo.share_sessions(tenant_id,share_id,collection_id,session_hash,link_version,expires_at,locale,csrf_hash) VALUES($1,$2,$3,$4,$5,clock_timestamp()+interval '1 hour','en',$6) RETURNING id`,[]any{tid,share.ID,collection,sessionHash,policyVersion,csrfHash},&guestSession)
 portalConfig,e:=pgxpool.ParseConfig(dsn);if e!=nil{t.Fatal(e)};portalConfig.ConnConfig.RuntimeParams["role"]="photo_portal";portal,e:=pgxpool.NewWithConfig(ctx,portalConfig);if e!=nil{t.Fatal(e)};defer portal.Close()
 pendingIntent,usedIntent:=uuid.New(),uuid.New();pendingToken,usedToken:=[]byte(strings.ReplaceAll(uuid.NewString(),"-","")),[]byte(strings.ReplaceAll(uuid.NewString(),"-",""));pendingKey,usedKey:=uuid.NewString(),uuid.NewString();intentExpiry:=time.Now().Add(50*time.Second)
 createIntent:=func(key string,id uuid.UUID,token []byte)(string,uuid.UUID){t.Helper();var outcome string;var got *uuid.UUID;var expires *time.Time;if err:=portal.QueryRow(ctx,`SELECT outcome,intent_id,expires_at FROM photo.create_portal_item_download_intent($1,$2,$3,$4,$5,$6,$7,$8,$9)`,sessionHash,csrfHash,key,sha,item,preset,id,token,intentExpiry).Scan(&outcome,&got,&expires);err!=nil{t.Fatal(err)};if got==nil{return outcome,uuid.Nil};return outcome,*got}
 if outcome,id:=createIntent(pendingKey,pendingIntent,pendingToken);outcome!="created"||id!=pendingIntent{t.Fatal("real pending receipt",outcome,id)}
 if outcome,id:=createIntent(usedKey,usedIntent,usedToken);outcome!="created"||id!=usedIntent{t.Fatal("real consumable receipt",outcome,id)}
 var redeemed string;if e=portal.QueryRow(ctx,`SELECT outcome FROM photo.redeem_download_intent($1,'photo')`,usedToken).Scan(&redeemed);e!=nil||redeemed!="ok"{t.Fatal("positive guest redemption",redeemed,e)}
 var objectKey string;query(`SELECT object_key FROM photo.artifacts WHERE tenant_id=$1 AND id=$2`,[]any{tid,artifact},&objectKey)
 var signingCurrent bool;if e=portal.QueryRow(ctx,`SELECT photo.v30_delivery_download_intent_current($1,'photo',$2,'v1',$3,100,'image/jpeg')`,usedToken,objectKey,sha).Scan(&signingCurrent);e!=nil||!signingCurrent{t.Fatal("positive post-sign authority",e,signingCurrent)}
 mediaCommand:=func()(uuid.UUID,uuid.UUID){t.Helper();query(`SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`,[]any{tid,collection},&revision)
  selected,err:=collectionService.CreateSelection(ctx,who,tid,collection,uuid.NewString(),collections.SelectionRequest{CollectionRevision:strconv.FormatInt(revision,10),Scope:"selected",ItemIDs:[]uuid.UUID{item}});if err!=nil{t.Fatal(err)};var snap collections.SnapshotRef;if err=json.Unmarshal(selected.Body,&snap);err!=nil{t.Fatal(err)}
  accepted,err:=collectionService.StartBulk(ctx,who,tid,collection,uuid.NewString(),collections.BulkRequest{ExpectedRevision:revision,SelectionSnapshotID:snap.ID,Action:"retry_media",Params:json.RawMessage(`{"variants":["thumbnail"]}`)});if err!=nil{t.Fatal(err)};var ticket collections.BulkAccepted;if err=json.Unmarshal(accepted.Body,&ticket);err!=nil{t.Fatal(err)};event:=uuid.New();exec(`UPDATE photo.bulk_operations SET pending_event_id=$3 WHERE tenant_id=$1 AND id=$2`,tid,ticket.BulkOperationID,event)
  tx,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx));if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_worker`);err!=nil{t.Fatal(err)};var done bool;if err=tx.QueryRow(ctx,`SELECT photo.process_bulk_chunk($1,$2)`,ticket.BulkOperationID,event).Scan(&done);err!=nil||!done{t.Fatal(err,done)};if err=tx.Commit(ctx);err!=nil{t.Fatal(err)}
  var command uuid.UUID;query(`SELECT event_id FROM photo.outbox WHERE tenant_id=$1 AND routing_key='media.retry' AND payload->>'bulk_id'=$2 AND payload->>'item_id'=$3`,[]any{tid,ticket.BulkOperationID.String(),item.String()},&command);return ticket.BulkOperationID,command
 }
 acceptedBulk,acceptedEvent:=mediaCommand();delayedBulk,delayedEvent:=mediaCommand();racingBulk,racingEvent:=mediaCommand()
 acceptMedia:=func(bulk,event uuid.UUID)bool{t.Helper();tx,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx));if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_worker`);err!=nil{t.Fatal(err)};var accepted bool;if err=tx.QueryRow(ctx,`SELECT photo.accept_media_retry_command($1,$2,$3,$4,'["thumbnail"]')`,event,bulk,item,next).Scan(&accepted);err!=nil{t.Fatal(err)};if err=tx.Commit(ctx);err!=nil{t.Fatal(err)};return accepted}
 if !acceptMedia(acceptedBulk,acceptedEvent){t.Fatal("positive exact media intent")}
 // Restore source membership through actual RemoveItem/addition. Dependent
 // publication metadata cascades; immutable source evidence remains intact.
 query(`SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`,[]any{tid,collection},&revision)
 // NEW guest intent A and exact media intent C hold their membership
 // SHARE locks. Actual RemoveItem B must wait until both insertions commit.
 racingIntent,racingToken:=uuid.New(),[]byte(strings.ReplaceAll(uuid.NewString(),"-",""));racingKey:=uuid.NewString()
 portalInsert,e:=portal.Begin(ctx);if e!=nil{t.Fatal(e)};defer portalInsert.Rollback(context.WithoutCancel(ctx))
 var racingOutcome string;if e=portalInsert.QueryRow(ctx,`SELECT outcome FROM photo.create_portal_item_download_intent($1,$2,$3,$4,$5,$6,$7,$8,$9)`,sessionHash,csrfHash,racingKey,sha,item,preset,racingIntent,racingToken,intentExpiry).Scan(&racingOutcome);e!=nil||racingOutcome!="created"{t.Fatal("racing new guest intent",e,racingOutcome)}
 mediaInsert,e:=admin.Begin(ctx);if e!=nil{t.Fatal(e)};defer mediaInsert.Rollback(context.WithoutCancel(ctx));if _,e=mediaInsert.Exec(ctx,`SET LOCAL ROLE photo_worker`);e!=nil{t.Fatal(e)}
 var racingAccepted bool;if e=mediaInsert.QueryRow(ctx,`SELECT photo.accept_media_retry_command($1,$2,$3,$4,'["thumbnail"]')`,racingEvent,racingBulk,item,next).Scan(&racingAccepted);e!=nil||!racingAccepted{t.Fatal("racing new media intent",e,racingAccepted)}
 removalDone:=make(chan error,1);go func(){removalDone<-collectionService.RemoveItem(ctx,who,tid,collection,item,revision)}()
 removalDeadline:=time.Now().Add(3*time.Second);for{var waiting bool;query(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0)`,[]any{applicationName},&waiting);if waiting{break};if time.Now().After(removalDeadline){t.Fatal("actual removal did not wait for NEW item-bound inserts")};time.Sleep(10*time.Millisecond)}
 if e=portalInsert.Commit(ctx);e!=nil{t.Fatal(e)};if e=mediaInsert.Commit(ctx);e!=nil{t.Fatal(e)};if e=<-removalDone;e!=nil{t.Fatal("remove published-history membership after NEW inserts",e)}
 var racingRetained bool;query(`SELECT EXISTS(SELECT FROM photo.download_intents WHERE tenant_id=$1 AND id=$2 AND item_id=$3) AND EXISTS(SELECT FROM photo.media_retry_jobs WHERE tenant_id=$1 AND event_id=$4 AND item_id=$3)`,[]any{tid,racingIntent,item,racingEvent},&racingRetained);if !racingRetained{t.Fatal("racing accepted historical identity lost")}
 if e=portal.QueryRow(ctx,`SELECT outcome FROM photo.redeem_download_intent($1,'photo')`,racingToken).Scan(&redeemed);e!=nil||redeemed!="revoked"{t.Fatal("race-created bearer authorized removed membership",e,redeemed)}

 var publicationsGone bool;query(`SELECT NOT EXISTS(SELECT FROM photo.publications WHERE tenant_id=$1 AND item_id=$2)`,[]any{tid,item},&publicationsGone);if !publicationsGone{t.Fatal("removed membership retained access publication metadata")}
 if outcome,id:=createIntent(pendingKey,pendingIntent,pendingToken);outcome!="replayed"||id!=pendingIntent{t.Fatal("accepted historical receipt lost",outcome,id)}
 if e=portal.QueryRow(ctx,`SELECT outcome FROM photo.redeem_download_intent($1,'photo')`,pendingToken).Scan(&redeemed);e!=nil||redeemed!="revoked"{t.Fatal("removed item bearer usable",e,redeemed)}
 if e=portal.QueryRow(ctx,`SELECT photo.v30_delivery_download_intent_current($1,'photo',$2,'v1',$3,100,'image/jpeg')`,usedToken,objectKey,sha).Scan(&signingCurrent);e!=nil||signingCurrent{t.Fatal("removed item passed post-sign authority",e,signingCurrent)}
 if !acceptMedia(acceptedBulk,acceptedEvent){t.Fatal("existing historical media receipt no longer replays")};if acceptMedia(delayedBulk,delayedEvent){t.Fatal("first delayed removed-item event created job")}
 var retained bool;query(`SELECT EXISTS(SELECT FROM photo.download_intents WHERE tenant_id=$1 AND id=$2 AND item_id=$3) AND EXISTS(SELECT FROM photo.media_retry_jobs WHERE tenant_id=$1 AND event_id=$4 AND item_id=$3) AND NOT EXISTS(SELECT FROM photo.media_retry_jobs WHERE event_id=$5)`,[]any{tid,pendingIntent,item,acceptedEvent,delayedEvent},&retained);if !retained{t.Fatal("historical receipts deleted or delayed job admitted")}
 scheduler,e:=admin.Begin(ctx);if e!=nil{t.Fatal(e)};defer scheduler.Rollback(context.WithoutCancel(ctx));if _,e=scheduler.Exec(ctx,`SET LOCAL ROLE photo_scheduler`);e!=nil{t.Fatal(e)}
 // Put this retained job first without changing its immutable source identity.
 exec(`UPDATE photo.media_retry_jobs SET created_at='1970-01-01' WHERE event_id=$1`,acceptedEvent)
 rows,claimErr:=scheduler.Query(ctx,`SELECT event_id FROM photo.claim_media_retry_job()`);if claimErr!=nil{t.Fatal(claimErr)};if rows.Next(){rows.Close();t.Fatal("removed source materialized")};claimErr=rows.Err();rows.Close();if claimErr!=nil{t.Fatal(claimErr)};if e=scheduler.Commit(ctx);e!=nil{t.Fatal(e)}
 var cancelled bool;query(`SELECT status='cancelled' AND error_code='SOURCE_CHANGED' FROM photo.media_retry_jobs WHERE event_id=$1`,[]any{acceptedEvent},&cancelled);if !cancelled{t.Fatal("retained missing-source job not cancelled normally")}

 query(`INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position,moderation_state) VALUES($1,$2,$3,$4,0,'accepted') RETURNING id`,[]any{tid,collection,asset,version},&item)
 exec(`UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,collection)
 // Publication A locks the exact current item until commit. Replacement B
 // must wait; afterwards A remains an historical publication, never a leak of B.
 publicationTx,e:=admin.Begin(ctx);if e!=nil{t.Fatal(e)};defer publicationTx.Rollback(context.WithoutCancel(ctx))
 if _,e=publicationTx.Exec(ctx,`INSERT INTO photo.publications(tenant_id,item_id,asset_version_id,state,published_by,published_at) VALUES($1,$2,$3,'published',$4,now())`,tid,item,version,who.UserID);e!=nil{t.Fatal(e)}
 query(`SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`,[]any{tid,collection},&revision)
 replacementDone:=make(chan error,1);go func(){_,err:=collectionService.ReplaceItemVersion(ctx,who,tid,collection,item,collections.ItemVersionReplace{ExpectedRevision:strconv.FormatInt(revision,10),ExpectedVersion:version,Version:next});replacementDone<-err}()
 deadlinePublication:=time.Now().Add(3*time.Second);for{var waiting bool;query(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0)`,[]any{applicationName},&waiting);if waiting{break};if time.Now().After(deadlinePublication){publicationTx.Rollback(context.WithoutCancel(ctx));t.Fatal("replacement did not wait for publication lock")};time.Sleep(10*time.Millisecond)}
 if e=publicationTx.Commit(ctx);e!=nil{t.Fatal(e)};if e=<-replacementDone;e!=nil{t.Fatal(e)}
 query(`SELECT asset_version_id FROM photo.publications WHERE tenant_id=$1 AND item_id=$2`,[]any{tid,item},&publishedVersion);if publishedVersion!=version{t.Fatal("concurrent replacement rewrote publication")}
 query(`SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`,[]any{tid,collection},&revision)
 if e=collectionService.RemoveItem(ctx,who,tid,collection,item,revision);e!=nil{t.Fatal("remove concurrent published membership",e)}
 query(`INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position,moderation_state) VALUES($1,$2,$3,$4,0,'accepted') RETURNING id`,[]any{tid,collection,asset,version},&item)
 exec(`UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,collection)
 tx,e:=admin.Begin(ctx);if e!=nil{t.Fatal(e)};defer tx.Rollback(context.WithoutCancel(ctx))
 if _,e=tx.Exec(ctx,`UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,collection);e!=nil{t.Fatal(e)}
 if _,e=tx.Exec(ctx,`DELETE FROM photo.collection_items WHERE tenant_id=$1 AND id=$2`,tid,item);e!=nil{t.Fatal(e)}
 if _,e=tx.Exec(ctx,`INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position,moderation_state) VALUES($1,$2,$3,$4,0,'accepted')`,tid,collection,asset,next);e!=nil{t.Fatal(e)}
 result:=make(chan error,1)
 go func(){p,err:=service.CollectionCullingGroups(ctx,who,tid,collection,review,"");if err==nil&&(len(p.Contexts)!=1||p.Contexts[0].Matched!=0||p.Contexts[0].Unmatched!=1||len(p.Contexts[0].Items)!=0){err=errCollectionCullingRace{}};result<-err}()
 // The projection waits for the collection CAS lock; commit then it must read
 // the new revision/member together, not expose the replaced historical frame.
 deadline:=time.Now().Add(3*time.Second);for {var waiting bool;query(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0)`,[]any{applicationName},&waiting);if waiting{break};if time.Now().After(deadline){tx.Rollback(context.WithoutCancel(ctx));t.Fatal("reader never reached collection row lock")};time.Sleep(10*time.Millisecond)}
 if e=tx.Commit(ctx);e!=nil{t.Fatal(e)};if e=<-result;e!=nil{t.Fatal(e)}
 exec(`UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`,who.SessionID)
 if _,e=service.CollectionCullingGroups(ctx,who,tid,collection,review,"");e==nil{t.Fatal("revoked actor session read projection")}
}
type errCollectionCullingRace struct{}
func(errCollectionCullingRace)Error()string{return "mixed current revision and historical member projection"}
