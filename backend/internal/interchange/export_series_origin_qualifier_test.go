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
// Mock receipts exercise orchestration only; production Python tests validate
// the complete schemas and cross-document series constraints.
func TestNativeSeriesOriginQualifierRequiresContentAndCurrentProducer(t *testing.T){
 for _,mode:=range []string{"valid","probe","metadata","bytes","changed","image_authority"}{t.Run(mode,func(t *testing.T){
  _,imageEntry,imageRaw:=importWorkingValidatorFixture(t)
  images:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{imageEntry.Ref.ArtifactID:{Entry:imageEntry,Body:imageRaw}}}
  imageAuthority:=&seriesStatImageStub{deny:mode=="image_authority"}
  stat,err:=canonical.Marshal(map[string]any{"image":imageEntry.Ref});if err!=nil{t.Fatal(err)};refs:=[]v2input.Ref{{ArtifactID:uuid.New(),SHA256:rawSHA(stat)},{ArtifactID:uuid.New(),SHA256:rawSHA(stat)}}
  params,err:=canonical.Marshal(map[string]any{"stats":refs});if err!=nil{t.Fatal(err)}
  source:=&seriesOriginStub{origin:ExportSeriesOriginEvidence{Task:uuid.New(),Attempt:1,Fence:1,Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)},Object:objectstore.Object{Key:"params",VersionID:"immutable",SHA256:rawSHA(params),Bytes:int64(len(params)),ContentType:"application/json"}},stat:objectstore.Object{Key:"stats",VersionID:"immutable",SHA256:rawSHA(stat),Bytes:int64(len(stat)),ContentType:"application/json"},change:mode=="changed"}
  reader:=ExportSeriesOriginReader{Source:source,Objects:seriesObjectsStub{bodies:map[string][]byte{"params":params,"stats":stat},bad:mode=="bytes"}}
  metadata:=&seriesAuthorityStub{};if mode=="metadata"{metadata.denyAt=1}
  root:=t.TempDir();scratch,err:=NewExportArchiveScratch(root,1<<20);if err!=nil{t.Fatal(err)}
  runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  qualifier,err:=NewNativeExportSeriesOriginQualifier(reader,metadata,ExportSeriesStatInputs{Authority:imageAuthority,Images:images,Measure:seriesMeasurePolicyStub{}},scratch,probe,map[string]string{},runtime);if err!=nil{t.Fatal(err)}
  raw:=[]byte(`{}`);entry:=v2input.Entry{Contract:"SeriesCorrections",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  err=qualifier.QualifyExportSeries(context.Background(),entry,raw)
  if mode=="valid"{if err!=nil||probe.calls!=1||metadata.calls!=2||source.calls!=6{t.Fatal("incomplete origin proof",err,source.calls)}}else if err==nil{t.Fatal("invalid origin accepted",mode)}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("native staging leaked",err)}
 })}
}

type seriesStatImageStub struct{deny bool;calls int}
func(s *seriesStatImageStub)QualifyExportSeriesStatImage(context.Context,ExportSeriesOriginEvidence,v2input.Entry,[]byte)error{s.calls++;if s.deny{return v2input.ErrInvalid};return nil}

type seriesMeasurePolicyStub struct{}
func(seriesMeasurePolicyStub)QualifyExportSeriesMeasure(context.Context,ExportSeriesOriginEvidence,v2input.Entry,[]byte,ExportClosureNode)error{return nil}
