package delivery

import (
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

// ReadDelegatedAncestorTask consumes the bounded cross-run ancestry predicate.
// Operation-specific claims and physical artifact bytes still need validation.
func ReadDelegatedAncestorTask(ctx context.Context,db *pgxpool.Pool,
 lease ExpansionLease,slot Slot,bucket string,
 requested uuid.UUID)(provenance.CanonicalSigningTask,error){
 var zero provenance.CanonicalSigningTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||requested==uuid.Nil{return zero,v2input.ErrInvalid}
 scope,ready,err:=ReadDelegatedReadyExport(ctx,db,lease,slot,bucket)
 if err!=nil{return zero,err}
 var raw []byte
 err=db.QueryRow(ctx,`SELECT photo.v31_delivery_ancestor_task_evidence($1,$2,$3,$4,$5)`,
  lease.Expansion,lease.Token,slot.Ordinal,slot.PresetID,requested).Scan(&raw)
 if err!=nil{return zero,err}
 task,err:=provenance.DecodeDelegatedAncestorTaskEvidence(raw,ready,requested)
 if err!=nil{return zero,err}
 if requested==ready.Task&&task.Run!=ready.Run{return zero,v2input.ErrInvalid}
 currentScope,currentReady,err:=ReadDelegatedReadyExport(ctx,db,lease,slot,bucket)
 if err!=nil{return zero,err}
 if currentScope!=scope||currentReady!=ready{return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return task,nil
}
