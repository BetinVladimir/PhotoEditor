package interchange

import (
 "bytes"
 "context"
 "io"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type seriesOriginStub struct{origin ExportSeriesOriginEvidence;stat objectstore.Object;calls int;change bool}
func(s *seriesOriginStub)LookupExportSeriesOrigin(context.Context,v2input.Entry,[]byte)(ExportSeriesOriginEvidence,error){s.calls++;out:=s.origin;if s.change&&s.calls>1{out.Fence++};return out,nil}
func(s *seriesOriginStub)LookupExportSeriesOriginDocument(context.Context,ExportSeriesOriginEvidence,v2input.Entry)(objectstore.Object,error){return s.stat,nil}
type seriesObjectsStub struct{bodies map[string][]byte;bad bool}
func(s seriesObjectsStub)Open(_ context.Context,object objectstore.Object)(io.ReadCloser,error){raw:=s.bodies[object.Key];if s.bad&&object.Key=="stats"{raw=[]byte(`[]`)};return io.NopCloser(bytes.NewReader(raw)),nil}
func TestSeriesOriginReaderPinsBytesAndRechecksProducer(t *testing.T){
 for _,mode:=range []string{"valid","changed","bytes","duplicate"}{t.Run(mode,func(t *testing.T){
  stat:=[]byte(`{}`);refs:=[]v2input.Ref{{ArtifactID:uuid.New(),SHA256:rawSHA(stat)},{ArtifactID:uuid.New(),SHA256:rawSHA(stat)}}
  if mode=="duplicate"{refs[1]=refs[0]}
  params,err:=canonical.Marshal(map[string]any{"stats":refs});if err!=nil{t.Fatal(err)}
  source:=&seriesOriginStub{origin:ExportSeriesOriginEvidence{Task:uuid.New(),Attempt:1,Fence:1,Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)},Object:objectstore.Object{Key:"params",VersionID:"immutable",SHA256:rawSHA(params),Bytes:int64(len(params)),ContentType:"application/json"}},stat:objectstore.Object{Key:"stats",VersionID:"immutable",SHA256:rawSHA(stat),Bytes:int64(len(stat)),ContentType:"application/json"},change:mode=="changed"}
  reader:=ExportSeriesOriginReader{Source:source,Objects:seriesObjectsStub{bodies:map[string][]byte{"params":params,"stats":stat},bad:mode=="bytes"}}
  raw:=[]byte(`{}`);entry:=v2input.Entry{Contract:"SeriesCorrections",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  bundle,err:=reader.ReadExportSeriesOrigin(context.Background(),entry,raw)
  if mode=="valid"{if err!=nil||len(bundle)==0||source.calls!=2{t.Fatal(err,source.calls)}}else if err==nil||bundle!=nil{t.Fatal("invalid original inputs returned",mode,err)}
 })}
}
