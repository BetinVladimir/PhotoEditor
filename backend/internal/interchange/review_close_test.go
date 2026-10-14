package interchange
import("context";"strings";"testing";"github.com/google/uuid";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/artifactjson";"photoeditor/backend/internal/canonical")
func TestImportCloseBindsFrozenIntentAndValidatesSQLReceipt(t *testing.T){
 tid,workflow,run:=uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 intent,err:=FreezeImportReviewIntent(context.Background(),tid,workflow,who,"close-1","close",[]byte(`{"expected_revision":"4","report_sha":"`+strings.Repeat("a",64)+`"}`));if err!=nil{t.Fatal(err)}
 raw,err:=canonical.Marshal(map[string]any{"run_id":run,"workflow_id":workflow,"revision":"5","state":"completed","outputs":[]any{},"error_code":nil});if err!=nil{t.Fatal(err)}
 tx:=&jobReadTx{raw:raw};selection:=artifactjson.Ref{ID:uuid.New(),SHA256:intent.DocumentSHA()}
 result,err:=intent.CommitClose(context.Background(),tx,selection);if err!=nil||len(tx.args)!=9||tx.args[0]!=tid||tx.args[1]!=workflow||tx.args[7]!=selection.ID{t.Fatal("commit binding",err)}
 tx.raw[0]='x';if result[0]!='{'{t.Fatal("borrowed receipt")}
 selection.SHA256=strings.Repeat("0",64);if _,err:=intent.CommitClose(context.Background(),tx,selection);err==nil{t.Fatal("selection substitution accepted")}
}
