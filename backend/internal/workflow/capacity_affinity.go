package workflow

import (
 "errors"
 "time"
 "github.com/google/uuid"
)

// CapacityAffinityEvidence is an adapter input, not residency authorization.
// Production wiring must supply current qualified worker residency; task JSON
// or a previously successful task cannot establish that a model is resident.
type CapacityAffinityEvidence struct {
 TaskID uuid.UUID
 ModelLockSHA string
 Resident bool
}

// SelectCapacityAffinity preserves the selected tenant and effective class.
// The oldest FIFO task may only be bypassed by a resident-model task at most
// 30 seconds newer. It never chooses a cheaper unrelated task or another tenant.
// CapacityDispatcher supplies current qualified SQL residency through
// ReadCapacityAffinity; this pure helper independently preserves ordering.
func SelectCapacityAffinity(candidates []CapacityCandidate, evidence []CapacityAffinityEvidence, fifo uuid.UUID, now time.Time)(uuid.UUID,error){
 if fifo==uuid.Nil||now.IsZero(){return uuid.Nil,errors.New("invalid affinity selection")}
 pins:=make(map[uuid.UUID]CapacityAffinityEvidence,len(evidence))
 for _,e:=range evidence{
  if e.TaskID==uuid.Nil||!capacityPolicyDigest.MatchString(e.ModelLockSHA){return uuid.Nil,errors.New("invalid affinity evidence")}
  if _,exists:=pins[e.TaskID];exists{return uuid.Nil,errors.New("duplicate affinity evidence")}
  pins[e.TaskID]=e
 }
 byID:=make(map[uuid.UUID]CapacityCandidate,len(candidates))
 for _,c:=range candidates{
  if c.TaskID==uuid.Nil||c.TenantID==uuid.Nil{return uuid.Nil,errors.New("invalid affinity candidate")}
  if _,exists:=byID[c.TaskID];exists{return uuid.Nil,errors.New("duplicate affinity candidate")}
  if _,err:=CapacityEffectiveQueue(c.OriginalQueue,c.CreatedAt,now);err!=nil{return uuid.Nil,err}
  byID[c.TaskID]=c
 }
 for id:=range pins{if _,exists:=byID[id];!exists{return uuid.Nil,errors.New("foreign affinity evidence")}}
 head,exists:=byID[fifo];if !exists{return uuid.Nil,errors.New("missing FIFO affinity head")}
 class,_:=CapacityEffectiveQueue(head.OriginalQueue,head.CreatedAt,now)
 best:=head;resident:=pins[head.TaskID].Resident
 for _,c:=range candidates{
  current,_:=CapacityEffectiveQueue(c.OriginalQueue,c.CreatedAt,now)
  if c.TenantID!=head.TenantID||current!=class{continue}
  if c.CreatedAt.Before(head.CreatedAt)||(c.CreatedAt.Equal(head.CreatedAt)&&c.TaskID.String()<head.TaskID.String()){return uuid.Nil,errors.New("affinity input is not FIFO head")}
  if !pins[c.TaskID].Resident||c.CreatedAt.Sub(head.CreatedAt)>30*time.Second{continue}
  if !resident||c.CreatedAt.Before(best.CreatedAt)||(c.CreatedAt.Equal(best.CreatedAt)&&c.TaskID.String()<best.TaskID.String()){best=c;resident=true}
 }
 return best.TaskID,nil
}
