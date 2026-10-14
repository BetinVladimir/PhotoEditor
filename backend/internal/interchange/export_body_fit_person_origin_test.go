package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestBodyFitPersonProjectionKeepsSideIdentity(t *testing.T){
 for _,mode:=range []string{"before","after","unknown","foreign_image","changed_image","changed_fit"}{t.Run(mode,func(t *testing.T){
  _,_,image,mask:=bodyPersonFixture(t)
  after:=v2input.Ref{ArtifactID:uuid.New(),SHA256:image.Entry.Ref.SHA256}
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  body,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":after,"alignment":ref(),"before_pose":ref(),"after_pose":ref(),"before_person":mask,"after_person":mask},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)}},Body:body}
  side:="before";if mode=="after"{side="after";image.Entry.Ref=after}
  if mode=="unknown"{side="other"};if mode=="foreign_image"{image.Entry.Ref.ArtifactID=uuid.New()}
  if mode=="changed_image"{image.Body=[]byte(`{}`)};if mode=="changed_fit"{fit.Body=[]byte(`{}`)}
  actual,err:=exportBodyFitPersonProjection(fit,side,image)
  valid:=mode=="before"||mode=="after"
  if (err==nil)!=valid{t.Fatal("unexpected side projection",err)}
  if valid&&(actual.Image.ID!=image.Entry.Ref.ArtifactID||actual.Image.SHA256!=image.Entry.Ref.SHA256){t.Fatal("lost paired source")}
 })}
}
func TestBodyFitPersonOriginRequiresDatabaseAndContext(t *testing.T){
 s:=SQLExportDocumentCatalog{}
 if _,err:=s.LookupExportBodyFitPersonOrigin(context.Background(),v2input.Entry{},nil,ExportClosureNode{},"before",ExportClosureNode{});err==nil{t.Fatal("missing database accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=s.LookupExportBodyFitPersonOrigin(ctx,v2input.Entry{},nil,ExportClosureNode{},"before",ExportClosureNode{});err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
