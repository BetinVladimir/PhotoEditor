package workflow

import (
 "context"
 "errors"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

type CapacityHeadAffinity struct {
 Candidate CapacityCandidate
 ModelLockSHA string
 Resident bool
}

// ReadCapacityHeadAffinity returns exactly one current same-scope hint or the
// original head. The SQL source, not task JSON, establishes model residency.
// The canonical reservation still decides whether that task can be queued.
func ReadCapacityHeadAffinity(ctx context.Context,tx pgx.Tx,pool string,selection *CapacitySelection,now time.Time,policy CapacityQueuePolicy,excluded []uuid.UUID)(CapacityHeadAffinity,error){
 empty:=CapacityHeadAffinity{}
 if err:=ctx.Err();err!=nil{return empty,err}
 if tx==nil||selection==nil||selection.TaskID==uuid.Nil||selection.tenant==uuid.Nil||selection.createdAt.IsZero()||now.IsZero()||(pool!="cpu"&&pool!="gpu"&&pool!="signer")||policy.Validate()!=nil{return empty,errors.New("invalid capacity head affinity source")}
 if len(excluded)>100{return empty,errors.New("invalid capacity head exclusions")}
 seen:=make(map[uuid.UUID]bool,len(excluded))
 for _,id:=range excluded{if id==uuid.Nil||seen[id]{return empty,errors.New("invalid capacity head exclusions")};seen[id]=true}
 ownedExcluded:=make([]uuid.UUID,len(excluded));copy(ownedExcluded,excluded)
 rows,err:=tx.Query(ctx,`SELECT task_id,tenant_id,original_queue,created_at,estimated_ms,heavy,tenant_queued_heavy,model_lock_sha,resident
FROM photo.capacity_queue_head_affinity($1,$2,$3,$4,$5,$6,$7,$8)`,pool,selection.TaskID,selection.tenant,selection.class,selection.createdAt,now,policy.TenantHeavyQueued,ownedExcluded)
 if err!=nil{return empty,err};defer rows.Close()
 if !rows.Next(){if err=rows.Err();err!=nil{return empty,err};return empty,pgx.ErrNoRows}
 var result CapacityHeadAffinity
 var pin *string
 c:=&result.Candidate
 if err=rows.Scan(&c.TaskID,&c.TenantID,&c.OriginalQueue,&c.CreatedAt,&c.EstimatedMilliseconds,&c.Heavy,&c.TenantQueuedHeavy,&pin,&result.Resident);err!=nil{return empty,err}
 if rows.Next(){return empty,errors.New("duplicate capacity head affinity")}
 if err=rows.Err();err!=nil{return empty,err}
 if err=ctx.Err();err!=nil{return empty,err}
 class,e:=CapacityEffectiveQueue(c.OriginalQueue,c.CreatedAt,now);if e!=nil||class!=selection.class||c.TaskID==uuid.Nil||c.TenantID!=selection.tenant||c.Heavy&&c.TenantQueuedHeavy>=policy.TenantHeavyQueued{return empty,errors.New("changed capacity head affinity scope")}
 if _,e=CapacityTaskCharge(c.EstimatedMilliseconds,policy.UnknownCostSeconds);e!=nil{return empty,e}
 if result.Resident{
  if pin==nil||!capacityPolicyDigest.MatchString(*pin)||seen[c.TaskID]||c.CreatedAt.Before(selection.createdAt)||c.CreatedAt.Sub(selection.createdAt)>30*time.Second||c.CreatedAt.Equal(selection.createdAt)&&c.TaskID.String()<selection.TaskID.String(){return empty,errors.New("invalid capacity resident head")}
  result.ModelLockSHA=*pin
 }else if c.TaskID!=selection.TaskID||!c.CreatedAt.Equal(selection.createdAt)||pin!=nil{return empty,errors.New("changed capacity FIFO fallback")}
 return result,nil
}
