package delivery

import (
 "bytes"
 "context"

 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
 "github.com/google/uuid"
)

// DelegatedSigningExecutionReader must prove every canonical task/result,
// attempt/fence and operation-specific claim under the exact parent scope.
// It cannot return a graph supplied by a client or an unscoped catalog scan.
type DelegatedSigningExecutionReader interface {
 ReadDelegatedSigningExecution(context.Context,SignerDelegationScope,
  provenance.ReadySigningExport)(provenance.SigningExecutionGraph,error)
}

// CompileAndPublishDelegatedLineage keeps graph IO and object IO outside SQL
// admission transactions, requiring stable canonical graph evidence on both
// sides of immutable publication.
func CompileAndPublishDelegatedLineage(ctx context.Context,
 store SQLDelegatedLineageStore,reader DelegatedSigningExecutionReader,
 objects artifactjson.Objects)(v2input.Ref,[]byte,error){
 var zero v2input.Ref
 if err:=ctx.Err();err!=nil{return zero,nil,err}
 if reader==nil||objects==nil||store.Scope.ActorID==uuid.Nil||
  ValidateSignerDelegationScope(store.Lease,store.Slot,store.Scope)!=nil||
  store.Ready.Tenant!=store.Scope.TenantID||store.Ready.ExportID!=store.Scope.ExportID||
  store.Ready.Project!=store.Scope.ProjectID||
  store.Ready.Ref.ArtifactID!=store.Scope.ExportArtifactID||
  store.Ready.Ref.SHA256!=store.Scope.ExportSHA{return zero,nil,v2input.ErrInvalid}
 read:=func()([]byte,provenance.SigningExecutionGraph,error){
  graph,err:=reader.ReadDelegatedSigningExecution(ctx,store.Scope,store.Ready)
  if err!=nil{return nil,provenance.SigningExecutionGraph{},err}
  if graph.FinalTask!=store.Ready.Task||graph.Export!=store.Ready.Ref{
   return nil,provenance.SigningExecutionGraph{},v2input.ErrInvalid
  }
  raw,err:=canonical.Marshal(graph)
  if err!=nil||len(raw)<2||len(raw)>16<<20{
   return nil,provenance.SigningExecutionGraph{},v2input.ErrInvalid
  }
  return raw,graph,nil
 }
 first,graph,err:=read();if err!=nil{return zero,nil,err}
 body,err:=provenance.CompileSigningLineage(ctx,graph)
 if err!=nil{return zero,nil,err}
 hash:=digest(body)
 expected:=v2input.Ref{ArtifactID:uuid.NewSHA1(store.Scope.ExportID,
  []byte("signing:lineage:"+hash)),SHA256:hash}
 current,_,err:=read();if err!=nil{return zero,nil,err}
 if !bytes.Equal(first,current){return zero,nil,v2input.ErrInvalid}
 published,err:=PublishDelegatedSigningLineage(ctx,store,objects,expected,body)
 if err!=nil{return zero,nil,err}
 if published!=expected{return zero,nil,v2input.ErrInvalid}
 current,_,err=read();if err!=nil{return zero,nil,err}
 if !bytes.Equal(first,current){return zero,nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,nil,err}
 return expected,append([]byte(nil),body...),nil
}
