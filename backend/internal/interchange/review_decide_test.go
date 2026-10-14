package interchange
import("context";"strings";"testing";"github.com/google/uuid";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/artifactjson";"photoeditor/backend/internal/canonical")
func TestImportDecisionCommitRejectOnlyHasNoChildAndBindsIntent(t *testing.T){
 tid,workflow,entry:=uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 raw:=[]byte(`{"expected_revision":"3","report_sha":"`+strings.Repeat("a",64)+`","decisions":[{"entry_id":"`+entry.String()+`","decision":"reject"}]}`)
 intent,err:=FreezeImportReviewIntent(context.Background(),tid,workflow,who,"decide-1","decide",raw);if err!=nil{t.Fatal(err)}
 selectionRaw,err:=canonical.Marshal([]SelectedImportDecision{{Target:ImportReviewTarget{Entry:entry,State:"proposed"},Decision:"reject"}});if err!=nil{t.Fatal(err)}
 batch:=PreparedImportDecisionBatch{run:uuid.New(),project:uuid.New(),raw:selectionRaw}
 receipt,err:=canonical.Marshal(map[string]any{"workflow_id":workflow,"revision":"4","remaining":0,"child":nil,"batch_id":intent.owner});if err!=nil{t.Fatal(err)}
 tx:=&jobReadTx{raw:receipt};selection:=artifactjson.Ref{ID:uuid.New(),SHA256:intent.documentSHA}
 body,err:=intent.CommitDecisions(context.Background(),tx,selection,batch,nil)
 if err!=nil||len(tx.args)!=10||tx.args[0]!=tid||tx.args[1]!=workflow||tx.args[7]!=selection.ID||tx.args[9]!="[]"{t.Fatal("decision binding",err)}
 tx.raw[0]='x';if body[0]!='{'{t.Fatal("borrowed receipt")}
 if _,err:=intent.CommitDecisions(context.Background(),tx,selection,batch,[]ResolvedImportApproval{{Entry:entry}});err==nil{t.Fatal("unexpected approval receipt accepted")}
}
func TestImportDecisionCommitRefusesWrongResolvedEntry(t *testing.T){
 tid,workflow,entry:=uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeImportReviewIntent(context.Background(),tid,workflow,who,"decide-approve","decide",[]byte(`{"expected_revision":"1","report_sha":"`+strings.Repeat("a",64)+`","decisions":[{"entry_id":"`+entry.String()+`","decision":"approve"}]}`));if err!=nil{t.Fatal(err)}
 raw,_:=canonical.Marshal([]SelectedImportDecision{{Target:ImportReviewTarget{Entry:entry,State:"proposed"},Decision:"approve"}})
 batch:=PreparedImportDecisionBatch{run:uuid.New(),project:uuid.New(),raw:raw,approvals:1};tx:=&jobReadTx{}
 resolved:=[]ResolvedImportApproval{{Entry:uuid.New(),StreamRevision:"2",Artifact:uuid.New(),SHA:strings.Repeat("b",64)}}
 if _,err:=intent.CommitDecisions(context.Background(),tx,artifactjson.Ref{ID:uuid.New(),SHA256:intent.documentSHA},batch,resolved);err==nil{t.Fatal("foreign resolved entry accepted")}
 if tx.args!=nil{t.Fatal("invalid receipt reached SQL")}
}

func TestImportDecisionCommitRequiresStyleRegistrationBeforeSQL(t *testing.T){
 tid,workflow,entry:=uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeImportReviewIntent(context.Background(),tid,workflow,who,"missing-style","decide",[]byte(`{"expected_revision":"1","report_sha":"`+strings.Repeat("a",64)+`","decisions":[{"entry_id":"`+entry.String()+`","decision":"approve"}]}`));if err!=nil{t.Fatal(err)}
 raw,err:=canonical.Marshal([]SelectedImportDecision{{Target:ImportReviewTarget{Entry:entry,State:"proposed"},Decision:"approve"}});if err!=nil{t.Fatal(err)}
 batch:=PreparedImportDecisionBatch{run:uuid.New(),project:uuid.New(),raw:raw,approvals:1}
 tx:=&jobReadTx{}
 resolved:=[]ResolvedImportApproval{{Entry:entry,StreamRevision:"2",Artifact:uuid.New(),SHA:strings.Repeat("b",64)}}
 if _,err:=intent.CommitDecisions(context.Background(),tx,artifactjson.Ref{ID:uuid.New(),SHA256:intent.documentSHA},batch,resolved);err==nil{t.Fatal("missing registration accepted")}
 if tx.args!=nil{t.Fatal("missing registration reached SQL")}
}
