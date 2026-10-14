package workflow

import (
 "testing"
 "time"
 "strings"
 "github.com/google/uuid"
)

func TestCapacityAffinityThirtySecondBoundaryAndScope(t *testing.T){
 now:=time.Date(2026,10,12,1,0,0,0,time.UTC)
 head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"batch",CreatedAt:now.Add(-time.Minute)}
 for _,age:=range []time.Duration{29*time.Second,30*time.Second,30*time.Second+time.Nanosecond}{
  next:=head;next.TaskID=uuid.New();next.CreatedAt=head.CreatedAt.Add(age)
  evidence:=[]CapacityAffinityEvidence{{TaskID:next.TaskID,ModelLockSHA:strings.Repeat("a",64),Resident:true}}
  got,err:=SelectCapacityAffinity([]CapacityCandidate{next,head},evidence,head.TaskID,now)
  expected:=head.TaskID;if age<=30*time.Second{expected=next.TaskID}
  if err!=nil||got!=expected{t.Fatal(age,got,err)}
  other:=next;other.TenantID=uuid.New()
  if got,err:=SelectCapacityAffinity([]CapacityCandidate{other,head},evidence,head.TaskID,now);err!=nil||got!=head.TaskID{t.Fatal("cross tenant affinity",got,err)}
  other=next;other.OriginalQueue="interactive"
  if got,err:=SelectCapacityAffinity([]CapacityCandidate{other,head},evidence,head.TaskID,now);err!=nil||got!=head.TaskID{t.Fatal("cross class affinity",got,err)}
 }
 evidence:=[]CapacityAffinityEvidence{{TaskID:uuid.New(),ModelLockSHA:strings.Repeat("a",64),Resident:true}}
 if _,err:=SelectCapacityAffinity([]CapacityCandidate{head},evidence,head.TaskID,now);err==nil{t.Fatal("foreign residency accepted")}
 if _,err:=SelectCapacityAffinity([]CapacityCandidate{head,head},nil,head.TaskID,now);err==nil{t.Fatal("duplicate candidates accepted")}
 older:=head;older.TaskID=uuid.New();older.CreatedAt=head.CreatedAt.Add(-time.Second)
 if _,err:=SelectCapacityAffinity([]CapacityCandidate{head,older},nil,head.TaskID,now);err==nil{t.Fatal("non-FIFO input accepted")}
}
