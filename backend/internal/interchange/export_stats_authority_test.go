package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestSQLStatsAuthorityRejectsMissingScope(t *testing.T){
 for _,mode:=range []string{"missing-db","contract","hash","source","cancelled"}{t.Run(mode,func(t *testing.T){
  source:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("source"))}
  value:=map[string]any{"source_image":source,"feature_version":"stats20_v1","values":make([]float64,20),"coverage_bins":make([]bool,512),"mask_confidence":1,"warnings":[]string{}}
  if mode=="source"{value["source_image"]=nil}
  raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"Stats20"}
  ctx:=context.Background();switch mode{case "contract":entry.Contract="WorkingImage";case "hash":raw=[]byte("{}");case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  if err:=(SQLExportDocumentCatalog{}).QualifyExportStats(ctx,entry,raw);err==nil{t.Fatal("unqualified statistics accepted")}
  if _,err:=(SQLExportDocumentCatalog{}).LookupExportStatsOrigin(ctx,entry,raw);err==nil{t.Fatal("original Params exposed without authority")}
 })}
}
