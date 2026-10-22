package httpapi

// Work-only candidate. Real SQL/MinIO/provider/HTTP baseline, then exact pinned
// GetObject denial. This does not simulate a SQL locator mutation or claim
// actual version deletion, model quality, Apply admission or successful adoption.
import (
 "bytes"
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/json"
 "errors"
 "io"
 "net/http/httptest"
 "net/url"
 "os"
 "reflect"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/colorprofile"
 "photoeditor/backend/internal/config"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/semanticreview"
)

type previewExactAccessLoss struct {
 base *objectstore.Store
 denied *objectstore.Object
 hits int
}
func (s *previewExactAccessLoss) Open(ctx context.Context, obj objectstore.Object) (io.ReadCloser,error) {
 if err:=ctx.Err();err!=nil {return nil,err}
 if s.denied!=nil && obj==*s.denied {s.hits++;return nil,objectstore.ErrMissing}
 return s.base.Open(ctx,obj)
}

// Fixture is emitted by a real completed preview producer BEFORE Apply. It
// carries identities/typed Params only; provider resolves all authority itself.
func TestIntegrationPreviewPinnedAccessLossHTTP(t *testing.T) {
 path:=os.Getenv("PHOTO_TEST_PREVIEW_LATE_ACCESS_FIXTURE")
 if path=="" {t.Skip("fresh physical preview fixture required")}
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn=="" {t.Fatal("configured physical fixture needs database")}
 raw,err:=os.ReadFile(path);if err!=nil {t.Fatal(err)}
 var f struct {Family string `json:"family"`; Params json.RawMessage `json:"params"`; Actor uuid.UUID `json:"actor"`; Session uuid.UUID `json:"session"`; SRGB string `json:"srgb_sha"`}
 strict:=func(body []byte,target any) error {d:=json.NewDecoder(bytes.NewReader(body));d.DisallowUnknownFields();if e:=d.Decode(target);e!=nil{return e};if d.Decode(new(any))!=io.EOF{return errors.New("trailing fixture JSON")};return nil}
 if strict(raw,&f)!=nil || f.Actor==uuid.Nil || f.Session==uuid.Nil {t.Fatal("invalid physical fixture")}
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel()
 pool:=func(role string)*pgxpool.Pool {c,e:=pgxpool.ParseConfig(dsn);if e!=nil{t.Fatal(e)};if role!=""{c.ConnConfig.RuntimeParams["role"]=role};p,e:=pgxpool.NewWithConfig(ctx,c);if e!=nil{t.Fatal(e)};t.Cleanup(p.Close);return p}
 owner,api,authDB:=pool(""),pool("photo_api"),pool("photo_auth")
 var dbname string
 if e:=owner.QueryRow(ctx,"SELECT current_database()").Scan(&dbname);e!=nil || !strings.HasPrefix(dbname,"photo_test_") || len(dbname)!=43 {t.Fatal("owned private database required")}
 for _,c:=range dbname[len("photo_test_"):] {if !strings.ContainsRune("0123456789abcdef",c){t.Fatal("private database name")}}
 bucket:=os.Getenv("PHOTO_TEST_MINIO_BUCKET")
 real,e:=objectstore.New(objectstore.Config{Endpoint:os.Getenv("PHOTO_TEST_MINIO_URL"),Bucket:bucket,AccessKey:os.Getenv("PHOTO_TEST_MINIO_ACCESS"),SecretKey:os.Getenv("PHOTO_TEST_MINIO_SECRET"),Development:true});if e!=nil {t.Fatal(e)}
 storage:=&previewExactAccessLoss{base:real}
 lock,e:=colorprofile.LoadLockSHA("../../../resources/icc");if e!=nil {t.Fatal(e)}
 signed:=0
 sign:=func(ctx context.Context,o objectstore.Object,expiry time.Time)(string,error) {ttl:=time.Until(expiry);if ttl<=0 || ttl>14*time.Minute{return "",semanticreview.ErrInvalid};v,e:=real.SignRead(ctx,o,ttl+time.Second);if e!=nil{return "",e};if v.ExpiresAt.Before(expiry){return "",semanticreview.ErrInvalid};signed++;return v.URL,nil}
 who:=auth.Identity{UserID:f.Actor,SessionID:f.Session}
 var params semanticreview.ExposurePreviewParams
 var provider SemanticReviewPreviewProvider
 var run uuid.UUID
 switch f.Family {
 case "removal":
  var p semanticreview.RemovalPreviewParams
  if strict(f.Params,&p)!=nil {t.Fatal("removal Params")};if _,_,e:=semanticreview.FreezeRemovalPreviewParams(p);e!=nil{t.Fatal(e)};params=p.ExposurePreviewParams
  resolve,e:=semanticreview.SQLRemovalPreviewResolver(api,storage,bucket);if e!=nil{t.Fatal(e)}
  request:=semanticreview.PreviewRequest{ExpectedRevision:params.WorkflowRevision,TargetRevision:params.TargetRevision,PlanSHA:params.Plan.SHA256,CandidateSHA:params.Candidate.SHA256}
  lookup,e:=resolve(ctx,who,params.TenantID,params.IntentID,params.TargetID,request);if e!=nil{t.Fatal(e)};run=lookup.RunID
  provider,e=semanticreview.NewRemovalPreviewProvider(api,storage,bucket,resolve,sign,lock,f.SRGB);if e!=nil{t.Fatal(e)}
 case "background_cleanup":
  var p semanticreview.BackgroundCleanupPreviewParams
  if strict(f.Params,&p)!=nil{t.Fatal("cleanup Params")};if _,_,e:=semanticreview.FreezeBackgroundCleanupPreviewParams(p);e!=nil{t.Fatal(e)};params=p.ExposurePreviewParams
  resolve,e:=semanticreview.SQLBackgroundCleanupPreviewResolver(api,storage,bucket);if e!=nil{t.Fatal(e)}
  request:=semanticreview.PreviewRequest{ExpectedRevision:params.WorkflowRevision,TargetRevision:params.TargetRevision,PlanSHA:params.Plan.SHA256,CandidateSHA:params.Candidate.SHA256}
  lookup,e:=resolve(ctx,who,params.TenantID,params.IntentID,params.TargetID,request);if e!=nil{t.Fatal(e)};run=lookup.RunID
  provider,e=semanticreview.NewBackgroundCleanupPreviewProvider(api,storage,bucket,resolve,sign,lock,f.SRGB);if e!=nil{t.Fatal(e)}
 case "retouch":
  var p semanticreview.RetouchPreviewParams
  if strict(f.Params,&p)!=nil{t.Fatal("retouch Params")};if _,_,e:=semanticreview.FreezeRetouchPreviewParams(p);e!=nil{t.Fatal(e)};params=p.ExposurePreviewParams
  resolve,e:=semanticreview.SQLRetouchPreviewResolver(api,storage,bucket);if e!=nil{t.Fatal(e)}
  request:=semanticreview.PreviewRequest{ExpectedRevision:params.WorkflowRevision,TargetRevision:params.TargetRevision,PlanSHA:params.Plan.SHA256,CandidateSHA:params.Candidate.SHA256}
  lookup,e:=resolve(ctx,who,params.TenantID,params.IntentID,params.TargetID,request);if e!=nil{t.Fatal(e)};run=lookup.RunID
  provider,e=semanticreview.NewRetouchPreviewProvider(api,storage,bucket,resolve,sign,lock,f.SRGB);if e!=nil{t.Fatal(e)}
 default:t.Fatal("unknown physical preview family")
 }
 request:=semanticreview.PreviewRequest{ExpectedRevision:params.WorkflowRevision,TargetRevision:params.TargetRevision,PlanSHA:params.Plan.SHA256,CandidateSHA:params.Candidate.SHA256}
 var epoch int64
 if e:=owner.QueryRow(ctx,"SELECT auth_epoch FROM photo.users WHERE id=$1",f.Actor).Scan(&epoch);e!=nil{t.Fatal(e)}
 pub,priv,e:=ed25519.GenerateKey(rand.Reader);if e!=nil{t.Fatal(e)}
 service:=&auth.Service{DB:authDB,Pepper:make([]byte,32),Signer:auth.TokenSigner{Active:"fixture",Private:priv,Public:map[string]ed25519.PublicKey{"fixture":pub},Issuer:"fixture",Audience:"fixture"}}
 token,e:=service.Signer.Issue(f.Actor,f.Session,epoch);if e!=nil{t.Fatal(e)}
 handler:=(&Server{DB:api,Auth:service,SemanticReviewPreview:provider,Config:config.Config{Environment:"development",AppOrigin:"http://localhost:8080",PortalOrigin:"http://localhost:3000",MaxJSONBytes:1048576}}).Routes()
 query:=url.Values{"expected_revision":{params.WorkflowRevision},"target_revision":{params.TargetRevision},"plan_sha":{params.Plan.SHA256},"candidate_sha":{params.Candidate.SHA256}}
 get:=func()*httptest.ResponseRecorder {r:=httptest.NewRequest("GET","/api/v1/tenants/"+params.TenantID.String()+"/semantic-intents/"+params.IntentID.String()+"/targets/"+params.TargetID.String()+"/preview?"+query.Encode(),nil).WithContext(ctx);r.Header.Set("X-Photo-Client","native");r.Header.Set("Authorization","Bearer "+token);w:=httptest.NewRecorder();handler.ServeHTTP(w,r);return w}
 healthy:=func(t *testing.T){t.Helper();beforeSign:=signed;w:=get();var p semanticreview.Preview;if w.Code!=200 || json.Unmarshal(w.Body.Bytes(),&p)!=nil || p.Validate(params.TenantID,params.IntentID,params.TargetID,request,time.Now())!=nil || signed!=beforeSign+4 || w.Header().Get("Cache-Control")!="private, no-store" {t.Fatal("physical HTTP baseline failed",w.Code)}}
 healthy(t) // Actual bytes are verified and actual receipt sealed before the fault.
 roles:=[]string{"preview_presentation","working_image","pixels","preview_evidence","before","after","affected","protected"}
 snapshot:=func(t *testing.T)string {t.Helper();var s string;e:=owner.QueryRow(ctx,`SELECT jsonb_build_object(
 'run',(SELECT jsonb_build_object('status',status,'revision',revision,'cancel',cancel_requested,'config',config_snapshot->'semantic_retouch_preview','removal',v27_removal_preview_receipt,'cleanup',v27_background_cleanup_preview_receipt,'retouch',v27_retouch_preview_receipt) FROM photo.runs WHERE tenant_id=$1 AND id=$2),
 'target',(SELECT jsonb_build_object('state',state,'revision',revision,'child',child_run_id,'batch',batch_id) FROM photo.semantic_targets WHERE tenant_id=$1 AND id=$3),
 'events',(SELECT count(*) FROM photo.run_events WHERE tenant_id=$1),'outbox',(SELECT count(*) FROM photo.outbox WHERE tenant_id=$1),'inbox',(SELECT count(*) FROM photo.inbox WHERE tenant_id=$1),
 'runs',(SELECT count(*) FROM photo.runs WHERE tenant_id=$1),'tasks',(SELECT count(*) FROM photo.tasks WHERE tenant_id=$1),
 'usage',(SELECT jsonb_build_object('reserved',reserved_bytes,'storage',storage_bytes) FROM photo.tenant_usage WHERE tenant_id=$1))::text`,params.TenantID,run,params.TargetID).Scan(&s);if e!=nil{t.Fatal(e)};return s}
 for _,role:=range roles {t.Run(role,func(t *testing.T){
  var target objectstore.Object;var aid uuid.UUID;var storedBucket string
  e:=owner.QueryRow(ctx,`SELECT a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id WHERE t.tenant_id=$1 AND t.run_id=$2 AND a.output_role=$3 AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token`,params.TenantID,run,role).Scan(&aid,&storedBucket,&target.Key,&target.VersionID,&target.SHA256,&target.Bytes,&target.ContentType)
  if e!=nil || aid==uuid.Nil || storedBucket!=bucket || target.VersionID=="" || target.VersionID=="null" {t.Fatal("exact physical role pin unavailable",role)}
  before:=snapshot(t);signedBefore:=signed;hits:=storage.hits;storage.denied=&target
  defer func(){storage.denied=nil}()
  result,e:=provider.ReadPreview(ctx,who,params.TenantID,params.IntentID,params.TargetID,request)
  if !errors.Is(e,objectstore.ErrMissing) || !reflect.DeepEqual(result,semanticreview.Preview{}) || storage.hits<=hits || signed!=signedBefore {t.Fatal("physical loss did not fail closed",role)}
  w:=get();var leaked semanticreview.Preview
  if w.Code<400 || w.Code>599 || (json.Unmarshal(w.Body.Bytes(),&leaked)==nil && leaked.Validate(params.TenantID,params.IntentID,params.TargetID,request,time.Now())==nil) || signed!=signedBefore {t.Fatal("HTTP exposed denied physical preview",role,w.Code)}
  if snapshot(t)!=before {t.Fatal("physical loss changed publication or authority state",role)}
  var current objectstore.Object
  if e:=owner.QueryRow(ctx,"SELECT object_key,object_version,sha256,bytes,media_type FROM photo.artifacts WHERE tenant_id=$1 AND id=$2",params.TenantID,aid).Scan(&current.Key,&current.VersionID,&current.SHA256,&current.Bytes,&current.ContentType);e!=nil || current!=target {t.Fatal("immutable locator changed")}
  storage.denied=nil
  healthy(t) // Access restoration, not object rewriting, restores the real reader.
 })}
 t.Log("V27_PREVIEW_PINNED_ACCESS_LOSS_HTTP_OK")
}
