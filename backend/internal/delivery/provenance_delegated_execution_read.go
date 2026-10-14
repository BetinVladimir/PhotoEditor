package delivery

import (
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

// SQLDelegatedTaskCatalog binds every graph-node read to one frozen parent
// expansion and the exact ready slot. The SQL predicate proves ancestry.
type SQLDelegatedTaskCatalog struct {
 DB *pgxpool.Pool
 Lease ExpansionLease
 Slot Slot
 Scope SignerDelegationScope
 Ready provenance.ReadySigningExport
 Bucket string
}

func(s SQLDelegatedTaskCatalog)ReadDelegatedTask(ctx context.Context,
 id uuid.UUID)(provenance.CanonicalSigningTask,error){
 var zero provenance.CanonicalSigningTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||id==uuid.Nil||
  ValidateSignerDelegationScope(s.Lease,s.Slot,s.Scope)!=nil||
  s.Ready.Tenant!=s.Scope.TenantID||s.Ready.Project!=s.Scope.ProjectID||
  s.Ready.ExportID!=s.Scope.ExportID||
  s.Ready.Ref.ArtifactID!=s.Scope.ExportArtifactID||
  s.Ready.Ref.SHA256!=s.Scope.ExportSHA{
  return zero,v2input.ErrInvalid
 }
 current,err:=ReadSignerDelegationScope(ctx,s.DB,s.Lease,s.Slot)
 if err!=nil{return zero,err}
 if current!=s.Scope{return zero,v2input.ErrInvalid}
 task,err:=ReadDelegatedAncestorTask(ctx,s.DB,s.Lease,s.Slot,s.Bucket,id)
 if err!=nil{return zero,err}
 return task,nil
}

// BoundDelegatedSigningExecutionReader is usable only when a concrete
// operation-specific delegated claims resolver is installed.
type BoundDelegatedSigningExecutionReader struct {
 Catalog SQLDelegatedTaskCatalog
 Claims provenance.DelegatedTaskClaims
}

func(s BoundDelegatedSigningExecutionReader)ReadDelegatedSigningExecution(
 ctx context.Context,scope SignerDelegationScope,
 ready provenance.ReadySigningExport)(provenance.SigningExecutionGraph,error){
 var zero provenance.SigningExecutionGraph
 if s.Claims==nil||scope!=s.Catalog.Scope||ready!=s.Catalog.Ready{
  return zero,v2input.ErrInvalid
 }
 source:=provenance.BoundDelegatedSigningExecutionSource{
  Tasks:s.Catalog,Claims:s.Claims}
 return source.ReadDelegatedSigningExecution(ctx,ready)
}
