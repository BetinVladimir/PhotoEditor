package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestSQLLUTAuthorityRejectsUnqualifiedOccurrences(t *testing.T){
 for _,mode:=range []string{"missing-db","parent","array","hash","edge","source","cancelled"}{t.Run(mode,func(t *testing.T){
  array:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("lut"))},Contract:"AbsoluteLUTArray"}
  image:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))}
  raw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"image":image},"absolute_lut":map[string]any{"artifact":array.Ref}});if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"ResolvedStyle"}
  ctx:=context.Background()
  switch mode{case "parent":parent.Contract="StyleV2";case "array":array.Contract="WorkingPixels";case "hash":raw=[]byte("{}");case "edge":array.Ref.ArtifactID=uuid.New();case "source":raw=[]byte(`{"params":{"image":null},"absolute_lut":null}`);parent.Ref.SHA256=rawSHA(raw);case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  if err:=(SQLExportDocumentCatalog{}).QualifyExportLUT(ctx,parent,raw,array);err==nil{t.Fatal("unqualified LUT accepted")}
 })}
}
