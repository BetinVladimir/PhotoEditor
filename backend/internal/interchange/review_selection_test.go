package interchange
import("context";"encoding/json";"strings";"testing";"github.com/google/uuid";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/canonical";"photoeditor/backend/internal/v2input")
func TestImportDecisionSelectionRequiresExactProposedTarget(t *testing.T){
 tid,workflow,entry,version:=uuid.New(),uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};sha:=strings.Repeat("a",64)
 request:=[]byte(`{"expected_revision":"2","report_sha":"`+sha+`","decisions":[{"entry_id":"`+entry.String()+`","decision":"approve"}]}`)
 intent,err:=FreezeImportReviewIntent(context.Background(),tid,workflow,who,"decide-1","decide",request);if err!=nil{t.Fatal(err)}
 stream:=uuid.NewSHA1(workflow,[]byte("interchange:draft-stream:"+entry.String()));document:=uuid.NewSHA1(workflow,[]byte("interchange:draft-document:"+entry.String()));recipe:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 target:=ImportReviewTarget{Ordinal:0,Entry:entry,External:"photo-1",Version:&version,State:"proposed",Stream:&stream,Document:&document,Recipe:&recipe,Fields:[]json.RawMessage{}}
 for _,change:=range []string{"none","revision","report","terminal","source","entry"}{t.Run(change,func(t *testing.T){
  owned:=target;revision:="2";reportSHA:=sha
  switch change{case "revision":revision="3";case "report":reportSHA=strings.Repeat("b",64);case "terminal":owned.State="rejected";case "source":foreign:=uuid.New();owned.Stream=&foreign;case "entry":owned.Entry=uuid.New()}
  raw,err:=canonical.Marshal(map[string]any{"workflow_id":workflow,"project_id":tid,"run_id":version,"revision":revision,"review":map[string]any{"schema_version":1,"workflow_id":workflow,"project_id":tid,"task_id":uuid.NewSHA1(workflow,[]byte("v2.interchange.import:0")),"root":recipe,"report":v2input.Ref{ArtifactID:uuid.New(),SHA256:reportSHA},"targets":[]ImportReviewTarget{owned}}});if err!=nil{t.Fatal(err)}
  batch,err:=intent.selectDecisionContext(context.Background(),raw)
  if change!="none"{if err==nil{t.Fatal("stale selection accepted")};return}
  if err!=nil||batch.ApprovalCount()!=1{t.Fatal("selection",err)}
  body:=batch.Bytes();body[0]='x';if batch.Bytes()[0]!='['{t.Fatal("borrowed selection bytes")}
 })}
}
