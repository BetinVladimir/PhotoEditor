package workflow

import (
 "context"
 "errors"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

// DispatchHeadCycle uses the complete SQL-compressed FIFO inventory. A failed
// resident hint is excluded only for this cycle; the FIFO head remains until
// selected itself, stale, or no longer runnable. Every reservation remains a
// current SQL transaction and only a committed queue result charges DRR.
func(d *CapacityDispatcher) DispatchHeadCycle(ctx context.Context,limit int)(int,error){
 if d==nil||d.leader==nil||d.selector==nil||limit<1||limit>100{return 0,errors.New("invalid capacity head dispatch cycle")}
 var snapshot CapacityHeadSnapshot
 var nowTime time.Time
 err:=d.leader.Transaction(ctx,func(tx pgx.Tx)error{
  if e:=tx.QueryRow(ctx,"SELECT clock_timestamp()").Scan(&nowTime);e!=nil{return e}
  var e error;snapshot,e=ReadCapacityHeadSnapshot(ctx,tx,d.pool,nowTime,d.selector.policy);return e
 })
 if err!=nil{return 0,err}
 if err=d.selector.BeginHeadCycle(snapshot,nowTime);err!=nil{return 0,err}
 headsByID:=make(map[uuid.UUID]CapacityCandidate,len(snapshot.Heads))
 for _,candidate:=range snapshot.Heads{headsByID[candidate.TaskID]=candidate}
 count:=0
 excluded:=make([]uuid.UUID,0,limit)
 for attempt:=0;attempt<limit;attempt++{
  selection,e:=d.selector.nextHead(snapshot.Heads,nowTime);if e!=nil{return count,e};if selection==nil{break}
  head,found:=headsByID[selection.TaskID]
  if !found{return count,errors.New("missing capacity FIFO head")}
  queued,stale:=false,false
  selected:=head
  e=d.leader.Transaction(ctx,func(tx pgx.Tx)error{
   if d.policyPin!=""{if _,e:=ReadCapacityQueuePolicy(ctx,tx,d.policyPin);e!=nil{return e}}
   hint,e:=ReadCapacityHeadAffinity(ctx,tx,d.pool,selection,nowTime,d.selector.policy,excluded)
   if errors.Is(e,pgx.ErrNoRows){stale=true;return nil};if e!=nil{return e}
   replacement,e:=d.selector.ApplyHeadAffinity(selection,head,hint,nowTime);if e!=nil{return e}
   selection=replacement
   if replacement.TaskID==hint.Candidate.TaskID{selected=hint.Candidate}
   var id uuid.UUID;var class string;var profile []byte
   e=tx.QueryRow(ctx,"SELECT task_id,resource_class,profile FROM photo.capacity_pick_selected_task($1,$2,$3,$4)",selection.TaskID,selection.tenant,d.pool,d.selector.policy.TenantHeavyQueued).Scan(&id,&class,&profile)
   if errors.Is(e,pgx.ErrNoRows){return nil};if e!=nil{return e}
   if id!=selection.TaskID{return errors.New("capacity selected task mismatch")}
   available,e:=capacityHeavyAdmission(ctx,tx,d.policyPin,class);if e!=nil{return e};if !available{return nil}
   _,queued,e=(Dispatcher{PreferModelAffinity:true}).dispatchSelectedInTransaction(ctx,tx,id,profile);return e
  })
  if e!=nil{d.leader.Close();d.selector=nil;return count,e}
  if e=d.selector.Settle(selection,queued);e!=nil{d.leader.Close();d.selector=nil;return count,e}
  if queued{
   count++
   if selected.Heavy{for i:=range snapshot.Heads{if snapshot.Heads[i].TenantID==selection.tenant&&snapshot.Heads[i].Heavy{snapshot.Heads[i].TenantQueuedHeavy++}}}
  }
  if stale||selection.TaskID==head.TaskID{
   delete(headsByID,head.TaskID)
   for i:=range snapshot.Heads{if snapshot.Heads[i].TaskID==head.TaskID{snapshot.Heads=append(snapshot.Heads[:i],snapshot.Heads[i+1:]...);break}}
  }else if !queued{excluded=append(excluded,selection.TaskID)}
 }
 return count,nil
}
