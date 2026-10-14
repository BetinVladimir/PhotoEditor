package interchange

import (
 "context"
 "os"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Executes the production predicate against isolated relational fixtures; no
// migrations or application data are changed by this read-only integration test.
func TestIntegrationExportPromptWorkflowConsent(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated PostgreSQL required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 predicate:=strings.ReplaceAll(exportPromptWorkflowConsentSQL,"photo.workflow_consent_links","links")
 predicate=strings.ReplaceAll(predicate,"photo.consent_grants","grants")
 for _,tc:=range []struct{name string;tenant,project,workflow,grantProject int;status string;epoch int;present,linked,want bool}{
  {"current",1,2,3,2,"granted",7,true,true,true},
  {"revoked",1,2,3,2,"revoked",7,true,true,false},
  {"regranted_epoch",1,2,3,2,"granted",8,true,true,false},
  {"missing_grant",1,2,3,2,"granted",7,false,true,false},
  {"foreign_grant_project",1,2,3,9,"granted",7,true,true,false},
  {"foreign_link_project",1,9,3,9,"granted",7,true,true,false},
  {"other_tenant",9,2,3,2,"revoked",7,true,true,true},
  {"other_workflow",1,2,9,2,"revoked",7,true,true,true},
  {"no_dependency",1,2,3,2,"revoked",7,true,false,true},
 }{t.Run(tc.name,func(t *testing.T){
  query:=`WITH producer(tenant_id) AS (VALUES(1)),workflow(id,project_id) AS (VALUES(3,2)),
   links(tenant_id,project_id,workflow_id,consent_id,consent_epoch) AS
    (SELECT $1::int,$2::int,$3::int,4,7 WHERE $8::boolean),
   grants(tenant_id,project_id,id,status,epoch) AS
    (SELECT $1::int,$4::int,4,$5::text,$6::int WHERE $7::boolean)
   SELECT `+predicate+` FROM producer CROSS JOIN workflow`
  var allowed bool
  err:=db.QueryRow(ctx,query,tc.tenant,tc.project,tc.workflow,tc.grantProject,tc.status,tc.epoch,tc.present,tc.linked).Scan(&allowed)
  if err!=nil||allowed!=tc.want{t.Fatalf("allowed=%v want=%v error=%v",allowed,tc.want,err)}
 })}
}
