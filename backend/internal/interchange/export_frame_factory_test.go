package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
func TestSQLExportFrameFactoryConstructionOwnsSnapshotWithoutIO(t *testing.T){
 _,project,ref,raw,_:=exportPlanFixture(t);snapshot,err:=v2input.DecodePinnedSessionInputSnapshot(ref,raw);if err!=nil{t.Fatal(err)}
 sid:=snapshot.SnapshotID;request:=CreateRequest{ProjectID:project,Direction:"export",Format:"xmp",SnapshotID:&sid};who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 for _,mode:=range []string{"valid","missing_native","foreign_snapshot","cancelled"}{t.Run(mode,func(t *testing.T){
  objects:=&importReaderStub{};native:=&exportBinaryQualifierStub{};factory:=SQLExportProposalFrameFactory{DB:&pgxpool.Pool{},Objects:objects,Bucket:"test-bucket",Native:native,MaxBytes:1<<30};req:=request;ctx:=context.Background()
  switch mode{case "missing_native":factory.Native=nil;case "foreign_snapshot":foreign:=uuid.New();req.SnapshotID=&foreign;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  resolver,err:=factory.ExportProposalFrames(ctx,who,uuid.New(),req,"frame-key",ref,raw)
  if mode=="valid"{
   if err!=nil||resolver==nil{t.Fatal("valid construction",err)}
   foreign:=snapshot.Items[0];foreign.Binding.AssetVersionID=uuid.New()
   if _,err:=resolver.ResolveExportFrame(context.Background(),foreign);err==nil{t.Fatal("foreign item reached source")}
  }else if err==nil||resolver!=nil{t.Fatal("invalid construction exposed resolver",err)}
  if objects.opens!=0||native.calls!=0{t.Fatal("constructor performed IO/qualification")}
 })}
}
func TestRecipeResolveAndFrameEvidenceRequireDependencies(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,_,_,err:=(SQLExportRecipeEvidenceSource{}).Resolve(ctx,v2input.SessionSnapshotItem{});err!=context.Canceled{t.Fatal("cancelled resolve",err)}
 if _,_,_,err:=(SQLExportRecipeEvidenceSource{}).Resolve(context.Background(),v2input.SessionSnapshotItem{});err!=v2input.ErrInvalid{t.Fatal("missing recipe dependency",err)}
 if evidence,err:=(SQLExportFrameEvidenceSource{}).ReadExportFrameEvidence(context.Background(),v2input.SessionSnapshotItem{});err==nil||evidence.Frame.Content.ArtifactID!=uuid.Nil{t.Fatal("incomplete source exposed evidence",err)}
}
