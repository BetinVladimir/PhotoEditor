package provenance

import (
 "context"
 "bytes"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type SigningNegativeAdoptionReader interface {
 ReadSigningAdoptedNegativeRoot(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask)(v2input.Ref,error)
}

type SQLSigningNegativeQualificationReader struct {
 DB *pgxpool.Pool
 Adoption SigningNegativeAdoptionReader
 Documents SigningTaskDocumentReader
}

// Retained pins are read under current export authority and rechecked against
// adopted canonical result before and after physical Params/object operations.
// No historical success bypasses current project/session authorization.
func(s SQLSigningNegativeQualificationReader)ReadSigningNegativeQualification(ctx context.Context,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask)(v2optical.NegativeQualificationPin,error){
 var zero v2optical.NegativeQualificationPin
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||s.Adoption==nil||s.Documents==nil||task.Operation!="optical.negative"||task.Version!="1"||normalizeCanonicalSigningTask(&task)!=nil{return zero,ErrInvalid}
 roots,err:=canonicalTaskOutputRefs(task.Outputs)
 if err!=nil||len(roots)!=1{return zero,ErrInvalid}
 check:=func()error{
  root,err:=s.Adoption.ReadSigningAdoptedNegativeRoot(ctx,who,ready,task)
  if err!=nil{return err};if root!=roots[0]{return ErrInvalid};return ctx.Err()
 }
 if err:=check();err!=nil{return zero,err}
 params,err:=s.Documents.ReadSigningTaskDocument(ctx,who,ready,task,task.Params,65536)
 if err!=nil{return zero,err};if !pinned(params,task.Params,65536){return zero,ErrInvalid}
 params=append([]byte(nil),params...)
 var profile []byte
 err=tenant.WithTenant(ctx,s.DB,who,ready.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,`SELECT t.execution_profile FROM photo.tasks t
   JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
   JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
   WHERE t.tenant_id=$1 AND t.id=$2 AND t.run_id=$3 AND t.canonical_result_id=$4
   AND t.attempt=$5 AND t.fencing_token=$6 AND t.state='succeeded' AND NOT t.cancel_requested
   AND t.operation='optical.negative' AND t.operation_version='1' AND t.resource_class='cpu'
   AND w.project_id=$7 AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
   AND w.workflow_type='preview' AND w.renderer_version='v2.2'`,ready.Tenant,task.Task,task.Run,task.Result,task.Attempt,task.Fence,ready.Project).Scan(&profile)
 })
 if err!=nil{return zero,err}
 pin,err:=DecodeSigningNegativeQualification(profile,params)
 if err!=nil{return zero,err}
 current,err:=s.Documents.ReadSigningTaskDocument(ctx,who,ready,task,task.Params,65536)
 if err!=nil{return zero,err};if !bytes.Equal(current,params){return zero,ErrInvalid}
 if err:=check();err!=nil{return zero,err}
 return pin,nil
}

var _ SigningNegativeAdoptionReader=SQLSigningNegativeAdoptionReader{}
