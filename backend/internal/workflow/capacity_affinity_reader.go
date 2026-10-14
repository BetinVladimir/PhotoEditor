package workflow

import (
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

// ReadCapacityAffinity narrows SQL work to the selected scope and bypass window.
// The SQL source only reports models on currently eligible qualified nodes.
func ReadCapacityAffinity(ctx context.Context,tx pgx.Tx,candidates []CapacityCandidate,selection *CapacitySelection,now time.Time)([]CapacityAffinityEvidence,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if tx==nil||selection==nil||now.IsZero(){return nil,errors.New("invalid capacity affinity source")}
 var head *CapacityCandidate
 for i:=range candidates{if candidates[i].TaskID==selection.TaskID{head=&candidates[i];break}}
 if head==nil{return nil,errors.New("missing capacity affinity head")}
 headClass,err:=CapacityEffectiveQueue(head.OriginalQueue,head.CreatedAt,now);if err!=nil{return nil,err}
 if head.TenantID!=selection.tenant||headClass!=selection.class{return nil,errors.New("changed capacity affinity scope")}
 ids:=make([]uuid.UUID,0)
 requested:=make(map[uuid.UUID]bool,len(candidates))
 seenCandidates:=make(map[uuid.UUID]bool,len(candidates))
 for _,c:=range candidates{
  if c.TaskID==uuid.Nil||c.TenantID==uuid.Nil||seenCandidates[c.TaskID]{return nil,errors.New("invalid capacity affinity candidate")}
  seenCandidates[c.TaskID]=true
  class,err:=CapacityEffectiveQueue(c.OriginalQueue,c.CreatedAt,now);if err!=nil{return nil,err}
  if c.TenantID==selection.tenant&&class==selection.class&&(c.CreatedAt.Before(head.CreatedAt)||(c.CreatedAt.Equal(head.CreatedAt)&&c.TaskID.String()<head.TaskID.String())){return nil,errors.New("capacity affinity selection is not FIFO head")}
  if c.TenantID==selection.tenant&&class==selection.class&&!c.CreatedAt.Before(head.CreatedAt)&&c.CreatedAt.Sub(head.CreatedAt)<=30*time.Second{ids=append(ids,c.TaskID);requested[c.TaskID]=true}
 }
 rows,err:=tx.Query(ctx,"SELECT task_id,model_lock_sha,resident FROM photo.capacity_task_affinity($1)",ids)
 if err!=nil{return nil,err};defer rows.Close()
 result:=make([]CapacityAffinityEvidence,0)
 seenRows:=make(map[uuid.UUID]bool,len(ids))
 for rows.Next(){
  var item CapacityAffinityEvidence
  if err=rows.Scan(&item.TaskID,&item.ModelLockSHA,&item.Resident);err!=nil{return nil,err}
  if err=ctx.Err();err!=nil{return nil,err}
  if !requested[item.TaskID]||seenRows[item.TaskID]||!capacityPolicyDigest.MatchString(item.ModelLockSHA)||!item.Resident{return nil,errors.New("invalid capacity affinity SQL evidence")}
  seenRows[item.TaskID]=true
  result=append(result,item)
 }
 if err=rows.Err();err!=nil{return nil,err}
 if err=ctx.Err();err!=nil{return nil,err}
 return result,nil
}
