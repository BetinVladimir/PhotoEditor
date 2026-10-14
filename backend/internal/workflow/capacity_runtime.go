package workflow

import (
 "context"
 "errors"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

// CapacityRuntime owns all three pool selectors on one physical leader session.
// Loss of ownership is terminal for this instance; startup must create a new one.
type CapacityRuntime struct {
 leader *CapacityLeader
 dispatchers []*CapacityDispatcher
 pin string
}

func NewCapacityRuntime(ctx context.Context,pool *pgxpool.Pool,pin string)(*CapacityRuntime,error){
 if !capacityPolicyDigest.MatchString(pin){return nil,errors.New("invalid capacity runtime policy pin")}
 leader,err:=AcquireCapacityLeader(ctx,pool);if err!=nil{return nil,err}
 runtime:=&CapacityRuntime{leader:leader,pin:pin}
 var policy CapacityQueuePolicy
 err=leader.Transaction(ctx,func(tx pgx.Tx)error{
  if e:=CheckCapacitySchedulerContract(ctx,tx);e!=nil{return e}
  var e error;policy,e=ReadCapacityQueuePolicy(ctx,tx,pin);return e
 })
 if err!=nil{runtime.Close();return nil,err}
 for _,name:=range []string{"cpu","gpu","signer"}{
  dispatcher,e:=NewCapacityDispatcher(leader,policy,name);if e!=nil{runtime.Close();return nil,e}
  dispatcher.policyPin=pin
  runtime.dispatchers=append(runtime.dispatchers,dispatcher)
 }
 return runtime,nil
}

func(r *CapacityRuntime) Close(){
 if r==nil{return};if r.leader!=nil{r.leader.Close()};r.leader=nil;r.dispatchers=nil
}

func(r *CapacityRuntime) Dispatch(ctx context.Context,limit int)(int,error){
 if r==nil||r.leader==nil||len(r.dispatchers)!=3{return 0,ErrCapacityLeaderUnavailable}
 if limit<1||limit>100{return 0,errors.New("invalid capacity runtime dispatch limit")}
 // Operator revocation or SQL contract drift must stop every pool before
 // dispatch rather than reuse startup authority.
 err:=r.leader.Transaction(ctx,func(tx pgx.Tx)error{
  if e:=CheckCapacitySchedulerContract(ctx,tx);e!=nil{return e}
  _,e:=ReadCapacityQueuePolicy(ctx,tx,r.pin);return e
 })
 if err!=nil{r.Close();return 0,err}
 total:=0
 for _,dispatcher:=range r.dispatchers{
  count,e:=dispatcher.DispatchBoundedHeadCycle(ctx,limit);total+=count
  if e!=nil{r.Close();return total,e}
 }
 return total,nil
}
