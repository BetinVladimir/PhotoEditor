package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestExportWorkingValidatorPinsSnapshotCanvasICCAndBinaryRoles(t *testing.T){
 imported,entry,raw:=importWorkingValidatorFixture(t)
 image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
 receipt:=imported.masters[entry.Ref.ArtifactID]
 for _,mode:=range []string{"valid","canvas","icc","identity","hash"}{t.Run(mode,func(t *testing.T){
  expected:=exportWorkingBinding{ref:entry.Ref,canvas:receipt.canvas}
  validator:=ExportWorkingDocumentValidator{masters:map[uuid.UUID]exportWorkingBinding{entry.Ref.ArtifactID:expected},icc:image.Color.ICCSHA256}
  selected:=entry;body:=append([]byte(nil),raw...)
  switch mode{case "canvas":expected.canvas=rawSHA([]byte("foreign"));validator.masters[entry.Ref.ArtifactID]=expected;case "icc":validator.icc=rawSHA([]byte("foreign"));case "identity":selected.Ref.ArtifactID=uuid.New();case "hash":body=append(body,' ')}
  children,err:=validator.ExportDocumentChildren(context.Background(),selected,body)
  if mode=="valid"{
   if err!=nil||len(children)<1||children[0].Contract!="WorkingPixels"||children[0].Ref.ArtifactID!=image.Pixels.ID||children[0].Ref.SHA256!=image.Pixels.SHA256{t.Fatal("invalid working image",err)}
   for _,child:=range children{if child.Contract!="WorkingPixels"&&child.Contract!="WorkingAlpha"&&child.Contract!="DenseWarpField"{t.Fatal("opaque binary role")}}
  }else if err==nil{t.Fatal("unbound master accepted")}
 })}
 if _,err:=(ExportWorkingDocumentValidator{}).ExportDocumentChildren(context.Background(),v2input.Entry{Contract:"MaskRef"},[]byte(`{}`));err==nil{t.Fatal("missing dependency validator accepted")}
}
