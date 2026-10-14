package interchange

import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
// Mock native receipt proves orchestration; Python tests prove full schemas.
func TestNativeSeriesMeasureQualifierPinsOriginalParamsAcrossProbe(t *testing.T){
 for _,mode:=range []string{"valid","probe","changed","image"}{t.Run(mode,func(t *testing.T){
  _,imageEntry,imageRaw:=importWorkingValidatorFixture(t)
  raw,err:=canonical.Marshal(map[string]any{"image":imageEntry.Ref});if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Contract:"SeriesStats",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  params:=[]byte(`{}`);source:=&seriesMeasureOriginStub{origin:ExportSeriesMeasureOrigin{Task:uuid.New(),Attempt:1,Fence:1,Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)},Object:objectstore.Object{Key:"params",VersionID:"immutable",SHA256:rawSHA(params),Bytes:int64(len(params)),ContentType:"application/json"}},change:mode=="changed",changeAfter:2}
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,1<<20);if err!=nil{t.Fatal(err)}
  runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  qualifier,err:=NewNativeExportSeriesMeasureQualifier(ExportSeriesMeasureParamsReader{Source:source,Objects:statsParamsObjects{params}},seriesMeasureInputStub{},scratch,probe,map[string]string{},runtime);if err!=nil{t.Fatal(err)}
  node:=ExportClosureNode{Entry:imageEntry,Body:imageRaw};if mode=="image"{node.Entry.Ref.ArtifactID=uuid.New()}
  err=qualifier.QualifyExportSeriesMeasure(context.Background(),ExportSeriesOriginEvidence{},entry,raw,node)
  if mode=="valid"{if err!=nil||probe.calls!=1||source.calls!=4{t.Fatal("measure proof incomplete",err)}}else if err==nil{t.Fatal("invalid measure origin accepted",mode)}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("staging leak",err)}
 })}
}

type seriesMeasureInputStub struct{}
func(seriesMeasureInputStub)QualifyExportSeriesMeasureInputs(context.Context,ExportSeriesMeasureOrigin,v2input.Entry,[]byte,ExportClosureNode)error{return nil}
