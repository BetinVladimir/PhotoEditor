package interchange

import (
 "bytes"
 "context"
 "encoding/hex"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// ImportPreparedDocuments contains immutable bytes; it grants no admission or approval.
type ImportPreparedDocuments struct {
 identity ImportDocumentIdentity
 plan,params ImportDocument
 evidence *ImportDocument
}
type ImportDocumentPublisher interface {
 PublishImportDocument(context.Context,ImportDocumentIdentity,ImportDocument)(v2input.Ref,error)
}
func FreezeImportDocuments(identity ImportDocumentIdentity,parsed v2input.Ref,planBody []byte)(ImportPreparedDocuments,error){
 if identity.Run==uuid.Nil||identity.Workflow==uuid.Nil||parsed.ArtifactID==uuid.Nil||len(parsed.SHA256)!=64{return ImportPreparedDocuments{},objectstore.ErrInvalid}
 decoded,err:=hex.DecodeString(parsed.SHA256);if err!=nil||hex.EncodeToString(decoded)!=parsed.SHA256{return ImportPreparedDocuments{},objectstore.ErrInvalid}
 task:=uuid.NewSHA1(identity.Workflow,[]byte("v2.interchange.import:0"))
 planID:=uuid.NewSHA1(task,[]byte("interchange:plan:1"));paramsID:=uuid.NewSHA1(task,[]byte("interchange:params:1"))
 if parsed.ArtifactID==planID||parsed.ArtifactID==paramsID{return ImportPreparedDocuments{},objectstore.ErrInvalid}
 if len(planBody)<2||len(planBody)>8<<20{return ImportPreparedDocuments{},objectstore.ErrInvalid}
 body,err:=canonical.Bytes(planBody);if err!=nil||!bytes.Equal(body,planBody)||body[0]!='{'{return ImportPreparedDocuments{},objectstore.ErrInvalid}
 plan:=ImportDocument{Purpose:"v30_interchange_import_plan",Ref:v2input.Ref{ArtifactID:planID,SHA256:rawSHA(body)},Body:append([]byte(nil),body...)}
 raw,err:=canonical.Marshal(struct{
  SchemaVersion int `json:"schema_version"`
  Parsed v2input.Ref `json:"parsed"`
  Plan v2input.Ref `json:"plan"`
  MaxReportBytes int `json:"max_report_bytes"`
 }{1,parsed,plan.Ref,8<<20});if err!=nil{return ImportPreparedDocuments{},err}
 params:=ImportDocument{Purpose:"task_params",Ref:v2input.Ref{ArtifactID:paramsID,SHA256:rawSHA(raw)},Body:raw}
 return ImportPreparedDocuments{identity:identity,plan:plan,params:params},nil
}
func(d ImportPreparedDocuments)TaskID()uuid.UUID{return uuid.NewSHA1(d.identity.Workflow,[]byte("v2.interchange.import:0"))}
func(d ImportPreparedDocuments)Documents()[]ImportDocument{
 docs:=[]ImportDocument{{Purpose:d.plan.Purpose,Ref:d.plan.Ref,Body:append([]byte(nil),d.plan.Body...)}}
 if d.evidence!=nil{docs=append(docs,ImportDocument{Purpose:d.evidence.Purpose,Ref:d.evidence.Ref,Body:append([]byte(nil),d.evidence.Body...)})}
 return append(docs,ImportDocument{Purpose:d.params.Purpose,Ref:d.params.Ref,Body:append([]byte(nil),d.params.Body...)})
}
// Publish completes Plan before Params. Scheduler materialization must separately
// validate full closure and recheck current sources in its final transaction.
func(d ImportPreparedDocuments)Publish(ctx context.Context,publisher ImportDocumentPublisher)(v2input.Ref,error){
 if publisher==nil{return v2input.Ref{},objectstore.ErrInvalid}
 docs:=d.Documents()
 for _,doc:=range docs{
  if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
  if err:=validateImportStorageDocument(d.identity,doc);err!=nil{return v2input.Ref{},err}
 }
 for _,doc:=range docs{
  if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
  actual,err:=publisher.PublishImportDocument(ctx,d.identity,doc);if err!=nil{return v2input.Ref{},err}
  if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
  if actual!=doc.Ref{return v2input.Ref{},objectstore.ErrIntegrity}
 }
 return d.params.Ref,nil
}
