package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestBodyFitAlignmentEdgeRequiresExactDeclaredPair(t *testing.T){
 for _,mode:=range []string{"valid","foreign","contract","hash"}{t.Run(mode,func(t *testing.T){
  _,_,image,mask:=bodyPersonFixture(t)
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  alignment,after:=ref(),ref()
  raw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":after,"alignment":alignment,"before_pose":ref(),"after_pose":ref(),"before_person":mask,"after_person":mask},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}},Body:raw}
  child:=v2input.Entry{Ref:alignment,Contract:"PairAlignment"}
  if mode=="foreign"{child.Ref=ref()};if mode=="contract"{child.Contract="PoseSet"};if mode=="hash"{fit.Body=[]byte(`{}`)}
  inventory,err:=exportBodyFitAlignmentEdge(fit,child)
  if mode=="valid"{if err!=nil||inventory.Before!=image.Entry.Ref||inventory.After!=after{t.Fatal("pair identity lost",err)}}else if err==nil||inventory.Inputs!=nil{t.Fatal("foreign alignment accepted")}
 })}
}
func TestBodyFitAlignmentOriginRequiresSQLAndContext(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if _,err:=source.LookupExportBodyFitAlignmentOrigin(context.Background(),v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{});err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=source.LookupExportBodyFitAlignmentOrigin(ctx,v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{});err!=context.Canceled{t.Fatal(err)}
}
