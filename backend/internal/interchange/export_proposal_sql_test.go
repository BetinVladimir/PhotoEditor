package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
func TestSQLExportProposalInputsPreflight(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if ref,body,frames,err:=(SQLExportProposalInputs{}).ResolveExportProposalInputs(ctx,auth.Identity{},uuid.Nil,CreateRequest{},"key");err!=context.Canceled||ref.ArtifactID!=uuid.Nil||body!=nil||frames!=nil{t.Fatal("cancelled source returned data",err)}
 tid,who,intent,_,_:=exportExecutionFixture(t)
 if ref,body,frames,err:=(SQLExportProposalInputs{}).ResolveExportProposalInputs(context.Background(),who,tid,intent.Request,"pipeline-key");err!=v2input.ErrInvalid||ref.ArtifactID!=uuid.Nil||body!=nil||frames!=nil{t.Fatal("missing dependencies returned data",err)}
}
