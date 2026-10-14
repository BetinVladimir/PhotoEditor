package workflow

import (
 "testing"
 "time"
)

func TestCapacityTaskCharge(t *testing.T){
 if got,err:=CapacityTaskCharge(nil,1800);err!=nil||got!=1800{t.Fatal(got,err)}
 for _,tc:=range []struct{ms,want int64}{{0,1},{1,1},{1000,1},{1001,2},{9007199254740991,9007199254741}}{
  got,err:=CapacityTaskCharge(&tc.ms,1800);if err!=nil||got!=tc.want{t.Fatal(tc,got,err)}
 }
 for _,ms:=range []int64{-1,9007199254740992}{if _,err:=CapacityTaskCharge(&ms,1800);err==nil{t.Fatal("accepted invalid measurement",ms)}}
 for _,unknown:=range []int64{0,-1,9007199254740992}{if _,err:=CapacityTaskCharge(nil,unknown);err==nil{t.Fatal("accepted invalid default")}}
}

func TestCapacityQueueAgePromotesOnlyOnce(t *testing.T){
 created:=time.Date(2026,10,12,0,0,0,0,time.UTC)
 for _,tc:=range []struct{original,promoted string}{{"training","batch"},{"batch","capture"},{"capture","interactive"},{"interactive","interactive"}}{
  for _,age:=range []time.Duration{0,10*time.Minute-time.Nanosecond,10*time.Minute,24*time.Hour}{
   want:=tc.original;if age>=10*time.Minute{want=tc.promoted}
   got,err:=CapacityEffectiveQueue(tc.original,created,created.Add(age));if err!=nil||got!=want{t.Fatal(tc,age,got,err)}
  }
 }
 if _,err:=CapacityEffectiveQueue("training",created,created.Add(-time.Second));err==nil{t.Fatal("accepted future creation")}
 if _,err:=CapacityEffectiveQueue("unknown",created,created);err==nil{t.Fatal("accepted unknown class")}
}

func TestCapacityQueuePolicyExactClassSet(t *testing.T){
 p:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:1800,TenantHeavyQueued:2}
 if err:=p.Validate();err!=nil{t.Fatal(err)}
 delete(p.Weights,"training");p.Weights["other"]=1
 if p.Validate()==nil{t.Fatal("accepted substituted class")}
}
