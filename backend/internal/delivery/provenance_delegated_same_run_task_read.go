package delivery

import (
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

// ReadDelegatedSameRunTask reads one task reached by same-run task edges.
// A complete execution reader must additionally prove cross-run content
// parents and the operation-specific immutable claims.
func ReadDelegatedSameRunTask(ctx context.Context, db *pgxpool.Pool,
 lease ExpansionLease, slot Slot, bucket string,
 requested uuid.UUID)(provenance.CanonicalSigningTask,error){
 var zero provenance.CanonicalSigningTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||requested==uuid.Nil{return zero,v2input.ErrInvalid}
 scope,ready,err:=ReadDelegatedReadyExport(ctx,db,lease,slot,bucket)
 if err!=nil{return zero,err}
 var raw []byte
 err=db.QueryRow(ctx,`SELECT photo.v31_delivery_same_run_task_evidence($1,$2,$3,$4,$5)`,
  lease.Expansion,lease.Token,slot.Ordinal,slot.PresetID,requested).Scan(&raw)
 if err!=nil{return zero,err}
 task,err:=provenance.DecodeDelegatedSameRunTaskEvidence(raw,ready,requested)
 if err!=nil{return zero,err}
 currentScope,currentReady,err:=ReadDelegatedReadyExport(ctx,db,lease,slot,bucket)
 if err!=nil{return zero,err}
 if currentScope!=scope||currentReady!=ready{return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return task,nil
}
