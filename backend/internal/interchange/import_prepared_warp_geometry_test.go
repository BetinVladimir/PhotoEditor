package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestPreparedWarpGeometryUsesStoredOccurrenceTarget(t *testing.T){
 scope:=preparedPublisherScope();scope.Contract="DenseWarpField"
 from:=review.Canvas{Width:6,Height:4,Orientation:1,SpaceID:"original",SourceSHA256:rawSHA([]byte("image"))}
 to:=from;to.Width=3;to.Height=2;to.SpaceID="intermediate:warp"
 occurrence:=ImportWarpOccurrence{Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("warp"))},From:from,To:to}
 digest,err:=canonical.Hash(struct{From,To review.Canvas;Target v2input.SessionVersionBinding}{from,to,scope.Binding});if err!=nil{t.Fatal(err)}
 ref:=v2input.Ref{ArtifactID:uuid.NewSHA1(scope.Workflow,[]byte("interchange:warp:"+scope.Entry.String()+":"+occurrence.Original.ArtifactID.String()+":"+digest)),SHA256:occurrence.Original.SHA256}
 for _,mode:=range []string{"valid","foreign_identity","changed_geometry","foreign_bytes","wrong_role"}{t.Run(mode,func(t *testing.T){
  owned:=occurrence;entry:=v2input.Entry{Ref:ref,Contract:"DenseWarpField"}
  switch mode{case "foreign_identity":entry.Ref.ArtifactID=uuid.New();case "changed_geometry":owned.To.Width++;case "foreign_bytes":entry.Ref.SHA256=rawSHA([]byte("foreign"));case "wrong_role":entry.Contract="MaskPixels"}
  raw,err:=canonical.Marshal(owned);if err!=nil{t.Fatal(err)}
  geometry,err:=preparedImportWarpGeometry(scope,entry,raw)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected occurrence geometry",err)}
  if mode=="valid"{hash,_:=canonical.Hash(to);if geometry.Width!=3||geometry.Height!=2||geometry.CanvasSHA!=hash||geometry.DType!="float32"{t.Fatal("source grid substituted for occurrence target")}}
 })}
}
func TestPreparedWarpGeometryInstallationFailsClosed(t *testing.T){
 if _,err:=(SQLPreparedImportWarpGeometry{}).ResolveExportArrayGeometry(context.Background(),v2input.Entry{});err==nil{t.Fatal("missing SQL geometry installed")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(SQLPreparedImportWarpGeometry{}).ResolveExportArrayGeometry(ctx,v2input.Entry{});err!=context.Canceled{t.Fatal("cancellation lost",err)}
 if _,err:=NewSQLPreparedImportWarpResolver(context.Background(),SQLPreparedImportCatalog{},nil,nil,nil,"",1024);err==nil{t.Fatal("missing native geometry authority installed")}
}
