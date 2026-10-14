package interchange

import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type seriesMaskSourceStub struct{object objectstore.Object;calls int;change bool;deny bool}
func(s *seriesMaskSourceStub)LookupExportSeriesMeasureMask(context.Context,ExportSeriesMeasureOrigin,v2input.Entry,[]byte,ExportClosureNode,v2input.Ref)(objectstore.Object,error){s.calls++;if s.deny{return objectstore.Object{},v2input.ErrInvalid};out:=s.object;if s.change&&s.calls>1{out.VersionID="changed"};return out,nil}
// Mock mask receipts cover orchestration; production array tests cover pixels.
func TestSeriesMeasureMasksRequirePhysicalBytesAndStableObject(t *testing.T){
 for _,mode:=range []string{"valid","bytes","probe","changed","source"}{t.Run(mode,func(t *testing.T){
  _,entry,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  maskBytes:=[]byte("mask");mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA(maskBytes)},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}
  raw,err:=canonical.Marshal(map[string]any{"image":entry.Ref,"regions":[]any{map[string]any{"mask":mask}}});if err!=nil{t.Fatal(err)}
  origin:=ExportSeriesMeasureOrigin{Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  source:=&seriesMaskSourceStub{object:objectstore.Object{Key:"mask",VersionID:"immutable",SHA256:mask.Artifact.SHA256,Bytes:int64(len(maskBytes)),ContentType:"application/x-npy"},change:mode=="changed",deny:mode=="source"}
  if mode=="bytes"{maskBytes=[]byte("oops")}
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,10000);if err!=nil{t.Fatal(err)}
  runtime:=rawSHA([]byte("runtime"));probe:=&exportMaskProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  qualifier:=NativeExportSeriesMeasureMasks{Inputs:seriesMeasureInputStub{},Source:source,Objects:statsParamsObjects{maskBytes},Scratch:scratch,Probe:probe,RuntimeSHA:runtime}
  stat:=v2input.Entry{Contract:"SeriesStats",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("stats"))}}
  err=qualifier.QualifyExportSeriesMeasureInputs(context.Background(),origin,stat,raw,ExportClosureNode{Entry:entry,Body:imageRaw})
  if mode=="valid"{if err!=nil||source.calls!=2{t.Fatal(err)}}else if err==nil{t.Fatal("unqualified physical mask accepted",mode)}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",err)}
 })}
}
