package provenance

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Read must prove canonical task result IDs, attempts/fences and operation
// claims under current export/project authority. Return stable graph ordering.
type SigningExecutionSource interface {
 ReadSigningExecution(context.Context,auth.Identity,ReadySigningExport)(SigningExecutionGraph,error)
}
// Publication must reserve/complete an immutable JSON artifact for this exact
// export/project/tenant; no generic document authority is implied by a ref.
type SigningLineagePublisher interface {
 PublishSigningLineage(context.Context,auth.Identity,ReadySigningExport,v2input.Ref,[]byte)(v2input.Ref,error)
}
type CompiledExportLineageSource struct {Execution SigningExecutionSource;Documents SigningLineagePublisher}
func(s CompiledExportLineageSource)ResolveFrozenExportLineage(ctx context.Context,who auth.Identity,ready ReadySigningExport)(v2input.Ref,[]byte,error){
 var zero v2input.Ref
 if err:=ctx.Err();err!=nil{return zero,nil,err}
 if s.Execution==nil||s.Documents==nil||ready.Tenant==uuid.Nil||ready.Project==uuid.Nil||ready.ExportID==uuid.Nil||ready.Task==uuid.Nil||!ref(ready.Ref)||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return zero,nil,ErrInvalid}
 // Own the entire nested graph before invoking further callbacks.
 read:=func()(SigningExecutionGraph,[]byte,error){
  graph,err:=s.Execution.ReadSigningExecution(ctx,who,ready);if err!=nil{return SigningExecutionGraph{},nil,err}
  if graph.FinalTask!=ready.Task||graph.Export!=ready.Ref{return SigningExecutionGraph{},nil,ErrInvalid}
  raw,err:=canonical.Marshal(graph);if err!=nil||len(raw)>16<<20{return SigningExecutionGraph{},nil,ErrInvalid}
  var owned SigningExecutionGraph;if json.Unmarshal(raw,&owned)!=nil{return SigningExecutionGraph{},nil,ErrInvalid}
  return owned,raw,nil
 }
 graph,evidence,err:=read();if err!=nil{return zero,nil,err}
 body,err:=CompileSigningLineage(ctx,graph);if err!=nil{return zero,nil,err}
 expected:=v2input.Ref{ArtifactID:uuid.NewSHA1(ready.ExportID,[]byte("signing:lineage:"+bodyDigest(body))),SHA256:bodyDigest(body)}
 // Recheck before external publication, and again afterward. A failed final
 // check leaves only a private/recoverable artifact, never signing authority.
 _,current,err:=read();if err!=nil{return zero,nil,err};if !bytes.Equal(current,evidence){return zero,nil,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,nil,err}
 receipt,err:=s.Documents.PublishSigningLineage(ctx,who,ready,expected,append([]byte(nil),body...));if err!=nil{return zero,nil,err}
 if receipt!=expected{return zero,nil,ErrInvalid}
 _,current,err=read();if err!=nil{return zero,nil,err};if !bytes.Equal(current,evidence){return zero,nil,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,nil,err}
 return expected,body,nil
}
var _ FrozenExportLineageSource=CompiledExportLineageSource{}
