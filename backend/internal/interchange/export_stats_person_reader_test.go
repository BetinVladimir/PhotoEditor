package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type statsPersonDocumentCatalogStub struct {calls int;mode string;refs map[uuid.UUID]string}
func(s *statsPersonDocumentCatalogStub)LookupExportStatsPersonDocument(_ context.Context,_ ExportStatsPersonOrigin,entry v2input.Entry)(objectstore.Object,error){
 s.calls++
 if s.mode=="revoked"&&s.calls>3{return objectstore.Object{},v2input.ErrInvalid}
 if s.refs[entry.Ref.ArtifactID]!=entry.Contract{return objectstore.Object{},v2input.ErrInvalid}
 object:=objectstore.Object{Key:entry.Contract,VersionID:"immutable",SHA256:entry.Ref.SHA256,Bytes:2,ContentType:"application/json"}
 if s.mode=="oversize"&&s.calls==3{object.Bytes=1<<21}
 if s.mode=="changed"&&s.calls>3{object.VersionID="changed"}
 return object,nil
}
func TestStatsPersonDocumentReaderBoundsAndRechecksWholeSet(t *testing.T){
 for _,mode:=range []string{"valid","oversize","changed","revoked","bytes","alias","cancelled"}{t.Run(mode,func(t *testing.T){
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("{}"))}}
  origin:=ExportStatsPersonOrigin{Root:ref(),Params:ref(),Report:ref()}
  catalog:=&statsPersonDocumentCatalogStub{mode:mode,refs:map[uuid.UUID]string{origin.Root.ArtifactID:"MaskRef",origin.Params.ArtifactID:"PersonParams",origin.Report.ArtifactID:"PersonReport"}}
  objects:=ImportDocumentObjects(statsParamsObjects{[]byte("{}")});if mode=="oversize"||mode=="alias"||mode=="cancelled"{objects=exportDefectNoIO{t}};if mode=="bytes"{objects=statsParamsObjects{[]byte("[]")}}
  ctx:=context.Background();if mode=="alias"{origin.Params=origin.Root};if mode=="cancelled"{var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  result,err:=(ExportStatsPersonDocumentReader{Catalog:catalog,Objects:objects}).ReadExportStatsPersonDocuments(ctx,origin)
  if mode=="valid"{if err!=nil||catalog.calls!=6||string(result.Root)!="{}"||string(result.Params)!="{}"||string(result.Report)!="{}"{t.Fatal("incomplete document read",err)}}else if err==nil||result.Root!=nil||result.Params!=nil||result.Report!=nil{t.Fatal("invalid partial evidence escaped",err)}
 })}
}
