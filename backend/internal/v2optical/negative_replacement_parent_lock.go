package v2optical

import (
 "context"
 "sort"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)

// LockReadyParentSources is the single caller-owned transaction input gate for
// a replacement wave. It validates every receipt before SQL, locks/reconciles
// the original project graph first, then rechecks all completed and retained
// parents in run/task UUID order. No IO, commit or task publication occurs.
// The caller must roll back every refusal; SQL replacement-membership and fresh
// operation qualification/admission are still required before a queue receipt.
func(p PreparedNegativeReplacementInputs)LockReadyParentSources(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays)([]PreparedReplacementParentSources,error){
 if err:=ctx.Err();err!=nil{return nil,err};if tx==nil{return nil,v2input.ErrInvalid}
 checked,_,_,err:=p.checked(ctx);if err!=nil{return nil,err}
 ready,err:=checked.ReadyParentSources(ctx,rebuilt,retained);if err!=nil{return nil,err}
 all:=make([]VerifiedAdoptedParentArrays,0,len(rebuilt)+len(retained));all=append(all,rebuilt...);all=append(all,retained...)
 // Validate full locator/MIME packets before the first SQL call, including
 // completed ancestors not directly consumed by this particular wave.
 for _,proof:=range all{if _,_,err:=adoptedParentProofPacket(ctx,checked.bucket,checked.who,checked.tenant,checked.project,proof);err!=nil{return nil,err}}
 sort.Slice(all,func(i,j int)bool{
  a,b:=all[i].selection.metadata,all[j].selection.metadata
  if a.Run!=b.Run{return a.Run.String()<b.Run.String()};return a.Task.String()<b.Task.String()
 })
 // This retains tenant/usage/project and authoritative original graph locks
 // before any new parent run is touched, preventing project graph mutation.
 if err:=checked.Lock(ctx,tx);err!=nil{return nil,err}
 for _,proof:=range all{
  if err:=LockAdoptedParentArrays(ctx,tx,checked.bucket,checked.who,checked.tenant,checked.project,proof);err!=nil{return nil,err}
 }
 if err:=checked.lockRebuiltMembership(ctx,tx,rebuilt);err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err};return ready,nil
}

// Called only after original graph and rebuilt parent run/task/source locks are
// retained. Deterministic IDs alone never substitute persisted membership.
func(p PreparedNegativeReplacementInputs)lockRebuiltMembership(ctx context.Context,tx pgx.Tx,rebuilt []VerifiedAdoptedParentArrays)error{
 topology,err:=p.Topology();if err!=nil{return err}
 nodes:=make(map[uuid.UUID]NegativeReplacementNode,len(topology.Nodes));for _,node:=range topology.Nodes{nodes[node.Task]=node}
 ordered:=append([]VerifiedAdoptedParentArrays(nil),rebuilt...)
 sort.Slice(ordered,func(i,j int)bool{return ordered[i].selection.metadata.Task.String()<ordered[j].selection.metadata.Task.String()})
 for _,proof:=range ordered{
  if err:=ctx.Err();err!=nil{return err}
  s:=proof.selection.metadata;node,known:=nodes[s.Task];if !known||node.Run!=s.Run{return v2input.ErrInvalid}
  var member bool
  if err:=tx.QueryRow(ctx,"SELECT photo.v29_negative_replacement_member($1,$2,$3,$4,$5,$6,$7,$8,$9)",p.tenant,p.project,p.who.UserID,p.who.SessionID,p.review,p.hash,node.OriginalTask,node.Task,node.Run).Scan(&member);err!=nil{return err}
  if !member{return v2input.ErrExpired}
 }
 return ctx.Err()
}
