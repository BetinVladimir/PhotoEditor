package httpapi

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/json"
 "net/http/httptest"
 "net/url"
 "io"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/config"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/semanticreview"
)

// Requires an isolated migrated DB and completed, current removal preview in
// versioned object storage. The file describes identities only, never authority.
func TestIntegrationCurrentRemovalPreviewHTTP(t *testing.T) {
 dsn,path:=os.Getenv("PHOTO_TEST_DATABASE_URL"),os.Getenv("PHOTO_TEST_REMOVAL_PREVIEW_HTTP_FIXTURE")
 if path=="" {t.Skip("isolated current removal preview fixture required")}
 if dsn=="" {t.Fatal("configured HTTP fixture requires PHOTO_TEST_DATABASE_URL")}
 raw,err:=os.ReadFile(path);if err!=nil {t.Fatal(err)}
 var fixture struct {
  Params semanticreview.RemovalPreviewParams `json:"params"`
  Actor uuid.UUID `json:"actor"`
  Session uuid.UUID `json:"session"`
  ICCLockSHA string `json:"icc_lock_sha"`
  SRGBSHA string `json:"srgb_sha"`
 }
 decoder:=json.NewDecoder(strings.NewReader(string(raw)));decoder.DisallowUnknownFields()
 if decoder.Decode(&fixture)!=nil || decoder.Decode(new(any))!=io.EOF || fixture.Actor==uuid.Nil || fixture.Session==uuid.Nil {t.Fatal("invalid removal fixture")}
 p:=fixture.Params
 if _,_,err:=semanticreview.FreezeRemovalPreviewParams(p);err!=nil {t.Fatal(err)}
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel()
 pool:=func(role string)*pgxpool.Pool {
  cfg,err:=pgxpool.ParseConfig(dsn);if err!=nil {t.Fatal(err)}
  if role!="" {cfg.ConnConfig.RuntimeParams["role"]=role}
  db,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil {t.Fatal(err)};t.Cleanup(db.Close);return db
 }
 owner,api:=pool(""),pool("photo_api")
 var epoch int64
 if err:=owner.QueryRow(ctx,"SELECT auth_epoch FROM photo.users WHERE id=$1",fixture.Actor).Scan(&epoch);err!=nil {t.Fatal(err)}
 pub,priv,err:=ed25519.GenerateKey(rand.Reader);if err!=nil {t.Fatal(err)}
 service:=&auth.Service{DB:pool("photo_auth"),Pepper:make([]byte,32),Signer:auth.TokenSigner{Active:"fixture",Private:priv,Public:map[string]ed25519.PublicKey{"fixture":pub},Issuer:"fixture",Audience:"fixture"}}
 token,err:=service.Signer.Issue(fixture.Actor,fixture.Session,epoch);if err!=nil {t.Fatal(err)}
 bucket:=os.Getenv("PHOTO_TEST_MINIO_BUCKET")
 store,err:=objectstore.New(objectstore.Config{Endpoint:os.Getenv("PHOTO_TEST_MINIO_URL"),Bucket:bucket,AccessKey:os.Getenv("PHOTO_TEST_MINIO_ACCESS"),SecretKey:os.Getenv("PHOTO_TEST_MINIO_SECRET"),Development:true});if err!=nil {t.Fatal(err)}
 resolve,err:=semanticreview.SQLRemovalPreviewResolver(api,store,bucket);if err!=nil {t.Fatal(err)}
 signed:=0
 provider,err:=semanticreview.NewRemovalPreviewProvider(api,store,bucket,resolve,func(ctx context.Context,o objectstore.Object,expiry time.Time)(string,error) {
  signed++
  result,err:=store.SignRead(ctx,o,time.Minute)
  if err!=nil {return "",err};if result.ExpiresAt.Before(expiry) {return "",semanticreview.ErrInvalid};return result.URL,nil
 },fixture.ICCLockSHA,fixture.SRGBSHA);if err!=nil {t.Fatal(err)}
 handler:=(&Server{DB:api,Auth:service,SemanticReviewPreview:provider,Config:config.Config{Environment:"development",AppOrigin:"http://localhost:8080",PortalOrigin:"http://localhost:3000",MaxJSONBytes:1048576}}).Routes()
 query:=url.Values{"expected_revision":{p.WorkflowRevision},"target_revision":{p.TargetRevision},"plan_sha":{p.Plan.SHA256},"candidate_sha":{p.Candidate.SHA256}}
 base:="/api/v1/tenants/"+p.TenantID.String()+"/semantic-intents/"+p.IntentID.String()+"/targets/"+p.TargetID.String()+"/preview"
 get:=func(access string,query url.Values)*httptest.ResponseRecorder {
  r:=httptest.NewRequest("GET",base+"?"+query.Encode(),nil).WithContext(ctx);r.Header.Set("X-Photo-Client","native");if access!="" {r.Header.Set("Authorization","Bearer "+access)}
  w:=httptest.NewRecorder();handler.ServeHTTP(w,r);return w
 }
 if w:=get("",query);w.Code!=401 || signed!=0 {t.Fatal("authentication bypass",w.Code,signed)}
 w:=get(token,query)
 var result semanticreview.Preview
 request:=semanticreview.PreviewRequest{ExpectedRevision:p.WorkflowRevision,TargetRevision:p.TargetRevision,PlanSHA:p.Plan.SHA256,CandidateSHA:p.Candidate.SHA256}
 if w.Code!=200 || json.Unmarshal(w.Body.Bytes(),&result)!=nil || result.Validate(p.TenantID,p.IntentID,p.TargetID,request,time.Now())!=nil || result.QualityStatus!="review" || signed!=4 || w.Header().Get("Cache-Control")!="private, no-store" {t.Fatal("current removal HTTP preview failed",w.Code,signed)}
 for _,key:=range []string{"expected_revision","target_revision","candidate_sha","plan_sha"} {
  changed:=url.Values{};for k,v:=range query {changed[k]=append([]string(nil),v...)}
  if key=="expected_revision" || key=="target_revision" {replacement:="9223372036854775806";if query.Get(key)==replacement {replacement="1"};changed.Set(key,replacement)} else {replacement:=strings.Repeat("0",64);if query.Get(key)==replacement {replacement=strings.Repeat("1",64)};changed.Set(key,replacement)}
  before:=signed;w:=get(token,changed)
  if w.Code<400 || w.Code>=500 || signed!=before {t.Fatal("stale query signed media",key,w.Code,signed-before)}
 }
}
