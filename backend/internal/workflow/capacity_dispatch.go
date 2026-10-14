package workflow

import (
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

// CapacityDispatcher composes leader, snapshots, DRR and canonical reservations.
// Create one per pool on the single leader loop; never call concurrently.
type CapacityDispatcher struct {
 leader *CapacityLeader
 selector *CapacityDRR
 pool string
 policyPin string
}

func NewCapacityDispatcher(leader *CapacityLeader,policy CapacityQueuePolicy,pool string)(*CapacityDispatcher,error){
 if leader==nil||leader.session==nil||(pool!="cpu"&&pool!="gpu"&&pool!="signer"){return nil,errors.New("invalid capacity dispatcher")}
 selector,err:=NewCapacityDRR(policy);if err!=nil{return nil,err}
 return &CapacityDispatcher{leader:leader,selector:selector,pool:pool},nil
}

func(d *CapacityDispatcher) DispatchCycle(ctx context.Context,limit int)(int,error){
 if d==nil||d.leader==nil||d.selector==nil||limit<1||limit>100{return 0,errors.New("invalid capacity dispatch cycle")}
 var candidates []CapacityCandidate
 var nowTime time.Time
 err:=d.leader.Transaction(ctx,func(tx pgx.Tx)error{
  var e error;candidates,e=ReadCapacityCandidates(ctx,tx,d.pool);if e!=nil{return e}
  // Capture after reading: a task committed between the two statements must
  // not appear to have a future creation time relative to the cycle clock.
  return tx.QueryRow(ctx,"SELECT clock_timestamp()").Scan(&nowTime)
 })
 if err!=nil{return 0,err}
 // Age classification uses the snapshot database clock.
 if err=d.selector.BeginCycle(candidates,nowTime);err!=nil{return 0,err}
 count:=0
 for attempt:=0;attempt<limit;attempt++{
  selection,e:=d.selector.Next(candidates,nowTime);if e!=nil{return count,e};if selection==nil{break}
  queued:=false
  e=d.leader.Transaction(ctx,func(tx pgx.Tx)error{
   // Hold installed immutable policy through reservation COMMIT; revocation
   // serializes with this transaction instead of racing a cached startup check.
   if d.policyPin!=""{if _,err:=ReadCapacityQueuePolicy(ctx,tx,d.policyPin);err!=nil{return err}}
   evidence,err:=ReadCapacityAffinity(ctx,tx,candidates,selection,nowTime);if err!=nil{return err}
   replacement,err:=d.selector.ApplyAffinity(selection,candidates,evidence,nowTime);if err!=nil{return err}
   selection=replacement
   var id uuid.UUID;var class string;var profile []byte
   err=tx.QueryRow(ctx,"SELECT task_id,resource_class,profile FROM photo.capacity_pick_selected_task($1,$2,$3,$4)",selection.TaskID,selection.tenant,d.pool,d.selector.policy.TenantHeavyQueued).Scan(&id,&class,&profile)
   if errors.Is(err,pgx.ErrNoRows){return nil};if err!=nil{return err}
   if id!=selection.TaskID{return errors.New("capacity selected task mismatch")}
   available,err:=capacityHeavyAdmission(ctx,tx,d.policyPin,class);if err!=nil{return err};if !available{return nil}
   _,accepted,err:=(Dispatcher{PreferModelAffinity:true}).dispatchSelectedInTransaction(ctx,tx,id,profile);queued=accepted;return err
  })
  if e!=nil{
   // Commit uncertainty cannot be reconciled by charging or retrying in memory.
   d.leader.Close();d.selector=nil;return count,e
  }
  if e=d.selector.Settle(selection,queued);e!=nil{d.leader.Close();d.selector=nil;return count,e}
  if queued{
   count++;selectedHeavy:=false
   for _,c:=range candidates{if c.TaskID==selection.TaskID{selectedHeavy=c.Heavy;break}}
   if selectedHeavy{for i:=range candidates{if candidates[i].TenantID==selection.tenant&&candidates[i].Heavy{candidates[i].TenantQueuedHeavy++}}}
  }
  for i,c:=range candidates{if c.TaskID==selection.TaskID{candidates=append(candidates[:i],candidates[i+1:]...);break}}
 }
 return count,nil
}
