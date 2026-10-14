package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type seriesMeasureOriginStub struct{origin ExportSeriesMeasureOrigin;calls int;change bool;changeAfter int}
func(s *seriesMeasureOriginStub)LookupExportSeriesMeasureOrigin(context.Context,ExportSeriesOriginEvidence,v2input.Entry,[]byte)(ExportSeriesMeasureOrigin,error){s.calls++;out:=s.origin;threshold:=s.changeAfter;if threshold==0{threshold=1};if s.change&&s.calls>threshold{out.Fence++};return out,nil}
func TestSeriesMeasureParamsReaderPinsAndRechecks(t *testing.T){
 for _,mode:=range []string{"valid","bytes","changed","version"}{t.Run(mode,func(t *testing.T){
  params:=[]byte(`{}`);source:=&seriesMeasureOriginStub{origin:ExportSeriesMeasureOrigin{Task:uuid.New(),Attempt:1,Fence:1,Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)},Object:objectstore.Object{Key:"params",VersionID:"immutable",SHA256:rawSHA(params),Bytes:int64(len(params)),ContentType:"application/json"}},change:mode=="changed"}
  if mode=="version"{source.origin.Object.VersionID="null"}
  streamed:=params;if mode=="bytes"{streamed=[]byte(`[]`)}
  reader:=ExportSeriesMeasureParamsReader{Source:source,Objects:statsParamsObjects{streamed}}
  raw:=[]byte(`{}`);entry:=v2input.Entry{Contract:"SeriesStats",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  origin,body,err:=reader.ReadExportSeriesMeasureParams(context.Background(),ExportSeriesOriginEvidence{},entry,raw)
  if mode=="valid"{if err!=nil||string(body)!=string(params)||origin!=source.origin||source.calls!=2{t.Fatal(err)}}else if err==nil||body!=nil||origin!=(ExportSeriesMeasureOrigin{}){t.Fatal("unqualified measure Params returned",mode,err)}
 })}
}
func TestSeriesMeasureOriginSQLRequiresDatabase(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if _,err:=source.LookupExportSeriesMeasureOrigin(context.Background(),ExportSeriesOriginEvidence{},v2input.Entry{},nil);err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=source.LookupExportSeriesMeasureOrigin(ctx,ExportSeriesOriginEvidence{},v2input.Entry{},nil);err!=context.Canceled{t.Fatal(err)}
}
