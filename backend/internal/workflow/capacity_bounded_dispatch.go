package workflow

import (
 "context"
 "errors"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

func(d *CapacityDispatcher) failBoundedCycle(count int,err error)(int,error){
 // The granted quantum and any committed reservations belong to this leader
 // generation. Reconstruct both from durable state after a source failure.
 d.leader.Close();d.selector=nil
 return count,err
}

// DispatchBoundedHeadCycle keeps only one SQL-selected head per reservation.
// The SQL choice considers the complete eligible queue on every read; neither
// tenant pagination nor a process-memory cutoff can alter DRR fairness.
func(d *CapacityDispatcher) DispatchBoundedHeadCycle(ctx context.Context,limit int)(int,error){
 if d==nil||d.leader==nil||d.selector==nil||limit<1||limit>100{return 0,errors.New("invalid bounded capacity dispatch cycle")}
 var nowTime time.Time
 var capChoice CapacityBoundedHeadChoice
 maximum:=make(map[string]int64,len(capacityClasses))
 for _,class:=range capacityClasses{maximum[class]=capacitySafeInteger}
 err:=d.leader.Transaction(ctx,func(tx pgx.Tx)error{
  if e:=tx.QueryRow(ctx,"SELECT clock_timestamp()").Scan(&nowTime);e!=nil{return e}
  var e error
  capChoice,e=ReadCapacityBoundedHeadChoice(ctx,tx,d.pool,nowTime,d.selector.policy,
   d.selector.lastTenant,d.selector.classCursor,maximum,nil)
  return e
 })
 if err!=nil{return d.failBoundedCycle(0,err)}
 if err=d.selector.BeginBoundedHeadCycle(capChoice.MaxRunnableCostSeconds,nowTime);err!=nil{return d.failBoundedCycle(0,err)}
 excludedHeads:=make([]uuid.UUID,0,limit)
 excludedHints:=make([]uuid.UUID,0,limit)
 count:=0
 for attempt:=0;attempt<limit;attempt++{
  var choice CapacityBoundedHeadChoice
  err=d.leader.Transaction(ctx,func(tx pgx.Tx)error{
   var e error
   choice,e=ReadCapacityBoundedHeadChoice(ctx,tx,d.pool,nowTime,d.selector.policy,
    d.selector.lastTenant,d.selector.classCursor,d.selector.deficits,excludedHeads)
   return e
  })
  if err!=nil{return d.failBoundedCycle(count,err)}
  selection,e:=d.selector.AcceptBoundedHead(choice,nowTime);if e!=nil{return d.failBoundedCycle(count,e)};if selection==nil{break}
  head:=*choice.Head
  queued,stale:=false,false
  selected:=head
  e=d.leader.Transaction(ctx,func(tx pgx.Tx)error{
   if d.policyPin!=""{if _,e:=ReadCapacityQueuePolicy(ctx,tx,d.policyPin);e!=nil{return e}}
   hint,e:=ReadCapacityHeadAffinity(ctx,tx,d.pool,selection,nowTime,d.selector.policy,excludedHints)
   if errors.Is(e,pgx.ErrNoRows){stale=true;return nil};if e!=nil{return e}
   replacement,e:=d.selector.ApplyHeadAffinity(selection,head,hint,nowTime);if e!=nil{return e}
   selection=replacement
   if replacement.TaskID==hint.Candidate.TaskID{selected=hint.Candidate}
   var id uuid.UUID;var class string;var profile []byte
   e=tx.QueryRow(ctx,"SELECT task_id,resource_class,profile FROM photo.capacity_pick_selected_task($1,$2,$3,$4)",selection.TaskID,selection.tenant,d.pool,d.selector.policy.TenantHeavyQueued).Scan(&id,&class,&profile)
   if errors.Is(e,pgx.ErrNoRows){return nil};if e!=nil{return e}
   if id!=selection.TaskID{return errors.New("capacity selected task mismatch")}
   available,e:=capacityHeavyAdmission(ctx,tx,d.policyPin,class);if e!=nil{return e};if !available{return nil}
   _,queued,e=(Dispatcher{PreferModelAffinity:true}).dispatchSelectedInTransaction(ctx,tx,id,profile)
   return e
  })
  if e!=nil{return d.failBoundedCycle(count,e)}
  if e=d.selector.Settle(selection,queued);e!=nil{return d.failBoundedCycle(count,e)}
  if queued{count++}
  if stale||selection.TaskID==head.TaskID{
   excludedHeads=append(excludedHeads,head.TaskID)
  }else if !queued{
   excludedHints=append(excludedHints,selected.TaskID)
  }
 }
 return count,nil
}
