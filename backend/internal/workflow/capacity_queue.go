package workflow

import (
 "errors"
 "time"
)

const capacitySafeInteger int64 = 9007199254740991

// CapacityQueuePolicy is installed by the scheduling leader, not supplied by a task.
// It does not itself attest leader ownership or authorize dispatch.
type CapacityQueuePolicy struct {
 Weights map[string]int64
 UnknownCostSeconds int64
 TenantHeavyQueued int64
}

func (p CapacityQueuePolicy) Validate() error {
 if len(p.Weights)!=4 || p.UnknownCostSeconds<1 || p.UnknownCostSeconds>capacitySafeInteger || p.TenantHeavyQueued<1 || p.TenantHeavyQueued>capacitySafeInteger {
  return errors.New("invalid capacity queue policy")
 }
 for _,class:=range []string{"interactive","capture","batch","training"} {
  if p.Weights[class]<1 || p.Weights[class]>100 {return errors.New("invalid capacity queue weight")}
 }
 return nil
}

// CapacityTaskCharge uses nil only for an absent measurement. Invalid measurements
// must not silently become the unknown-cost default. Ceil division cannot overflow.
func CapacityTaskCharge(estimatedMilliseconds *int64, unknownSeconds int64)(int64,error){
 if unknownSeconds<1 || unknownSeconds>capacitySafeInteger {return 0,errors.New("invalid unknown task cost")}
 if estimatedMilliseconds==nil {return unknownSeconds,nil}
 value:=*estimatedMilliseconds
 if value<0 || value>capacitySafeInteger {return 0,errors.New("invalid measured task cost")}
 seconds:=value/1000
 if value%1000!=0 {seconds++}
 if seconds<1 {seconds=1}
 return seconds,nil
}

// CapacityEffectiveQueue promotes once from the frozen original class; callers
// must never persist the promoted class back into the run snapshot.
func CapacityEffectiveQueue(original string, createdAt, now time.Time)(string,error){
 switch original {case "interactive","capture","batch","training":default:return "",errors.New("invalid capacity queue class")}
 if createdAt.IsZero() || now.IsZero() || createdAt.After(now) {return "",errors.New("invalid capacity queue age")}
 if now.Sub(createdAt)<10*time.Minute {return original,nil}
 switch original {case "training":return "batch",nil;case "batch":return "capture",nil;case "capture":return "interactive",nil}
 return original,nil
}
