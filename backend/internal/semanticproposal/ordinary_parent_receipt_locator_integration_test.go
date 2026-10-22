package semanticproposal

import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
)

// Uses the genuinely admitted ordinary command supplied by the HTTP fixture;
// does not construct a sealed graph or ledger response. This exercises retired
// object/producer independence of the SQL locator, not downstream adoption.
func TestIntegrationOrdinaryReceiptLocatorAfterSourceRetirement(t *testing.T) {
 dsn,raw:=os.Getenv("PHOTO_TEST_DATABASE_URL"),os.Getenv("PHOTO_TEST_SEMANTIC_CREATE_RECEIPT")
 if dsn==""||raw=="" {t.Skip("genuine committed ordinary HTTP admission fixture required")}
 var f struct{Accepted struct{Tenant,Actor,Session uuid.UUID;Key string;Intent v2input.SemanticEditIntent;Scene bool}}
 if json.Unmarshal([]byte(raw),&f)!=nil||f.Accepted.Tenant==uuid.Nil||f.Accepted.Actor==uuid.Nil||f.Accepted.Session==uuid.Nil||f.Accepted.Key=="" {t.Fatal("invalid admitted fixture")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil {t.Fatal(err)};defer db.Close()
 parent,err:=v2input.RunID(f.Accepted.Tenant,f.Accepted.Actor,f.Accepted.Key);if err!=nil {t.Fatal(err)}
 workflow,err:=v2input.WorkflowID(parent);if err!=nil {t.Fatal(err)}
 tx,err:=db.Begin(ctx);if err!=nil {t.Fatal(err)};defer tx.Rollback(ctx)
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",f.Accepted.Tenant.String());err!=nil {t.Fatal(err)}
 var project uuid.UUID;var body []byte
 if err=tx.QueryRow(ctx,`SELECT w.project_id,i.response_body FROM photo.v2_workflows w
 JOIN photo.idempotency_records i ON i.tenant_id=w.tenant_id AND i.resource_id=w.run_id
 AND i.principal_type='user' AND i.principal_id=$3 AND i.route='V2.Admit' AND i.key=$4
 WHERE w.tenant_id=$1 AND w.run_id=$2 AND i.response_status=202`,f.Accepted.Tenant,parent,f.Accepted.Actor,f.Accepted.Key).Scan(&project,&body);err!=nil {t.Fatal(err)}
 var ticket v2input.JobTicket
 if json.Unmarshal(body,&ticket)!=nil||ticket.RunID!=parent||ticket.WorkflowID!=workflow||ticket.State!="queued" {t.Fatal("fixture is not actual queued JobTicket wire",string(body))}
 if f.Accepted.Scene {
  var currentAnchor bool
  if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=$1
   AND id=photo.semantic_uuid_v5($2,'semantic.face.detect:reference')
   AND jsonb_typeof(config_snapshot->'v27_detector_prepare'->'recovery'->'scene_policy')='object')`,f.Accepted.Tenant,parent).Scan(&currentAnchor);err!=nil||!currentAnchor {t.Fatal("current scene detector anchor required",err)}
 }
 // Mutable retirement only; immutable payloads, digests and accepted command
 // remain untouched. All changes roll back with this isolated transaction.
 tag,err:=tx.Exec(ctx,`UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second'
 WHERE tenant_id=$1 AND id IN(SELECT master_artifact_id FROM photo.asset_versions
 WHERE tenant_id=$1 AND id=$2)`,f.Accepted.Tenant,f.Accepted.Intent.Reference.AssetVersionID)
 if err!=nil||tag.RowsAffected()!=1 {t.Fatal("source retirement fixture",err)}
 if _,err=tx.Exec(ctx,`UPDATE photo.v2_qualification_policies SET admission_enabled=false
 WHERE operation IN('semantic.transfer','scene.parse','depth.predict')`);err!=nil {t.Fatal(err)}
 if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil {t.Fatal(err)}
 var locator []byte
 if err=tx.QueryRow(ctx,"SELECT photo.ordinary_parent_accepted_receipt_locator($1,$2,$3,$4,$5)",f.Accepted.Tenant,project,f.Accepted.Actor,f.Accepted.Session,parent).Scan(&locator);err!=nil {t.Fatal("accepted locator depends on retired producer/source",err)}
 var got struct{Key string `json:"key"`;Run uuid.UUID `json:"run_id"`;Workflow uuid.UUID `json:"workflow_id"`;SHA string `json:"request_sha"`}
 if json.Unmarshal(locator,&got)!=nil||got.Key!=f.Accepted.Key||got.Run!=parent||got.Workflow!=workflow||len(got.SHA)!=64 {t.Fatal("wrong accepted locator",string(locator))}
 for _,scope:=range []struct{name string;tenant,project,actor,session uuid.UUID}{
  {"foreign tenant",uuid.New(),project,f.Accepted.Actor,f.Accepted.Session},
  {"foreign project",f.Accepted.Tenant,uuid.New(),f.Accepted.Actor,f.Accepted.Session},
  {"foreign actor",f.Accepted.Tenant,project,uuid.New(),f.Accepted.Session},
  {"foreign session",f.Accepted.Tenant,project,f.Accepted.Actor,uuid.New()},
 } {
  save,err:=tx.Begin(ctx);if err!=nil {t.Fatal(err)}
  var denied []byte
  err=save.QueryRow(ctx,"SELECT photo.ordinary_parent_accepted_receipt_locator($1,$2,$3,$4,$5)",scope.tenant,scope.project,scope.actor,scope.session,parent).Scan(&denied)
  if err==nil {t.Fatalf("%s recovered accepted command: %s",scope.name,denied)}
  if err=save.Rollback(ctx);err!=nil {t.Fatal(err)}
 }
 // A different deterministic parent/key has no original immutable metadata;
 // absence must remain absence rather than returning the accepted neighbour.
 foreign,err:=v2input.RunID(f.Accepted.Tenant,f.Accepted.Actor,uuid.NewString());if err!=nil {t.Fatal(err)}
 var absent []byte
 if err=tx.QueryRow(ctx,"SELECT photo.ordinary_parent_accepted_receipt_locator($1,$2,$3,$4,$5)",f.Accepted.Tenant,project,f.Accepted.Actor,f.Accepted.Session,foreign).Scan(&absent);err!=nil||len(absent)!=0 {t.Fatal("foreign parent/key inherited receipt",string(absent),err)}
}
