package v2optical

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestNegativeDeclinedDecisionPageRejectsIncompleteAndUnorderedWire(t *testing.T) {
 a:=uuid.MustParse("00000000-0000-0000-0000-000000000001")
 b:=uuid.MustParse("00000000-0000-0000-0000-000000000002")
 entry:=NegativeDeclinedDecision{Review:b,OriginTask:uuid.New(),OriginRun:uuid.New(),DecisionHash:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",Route:"base"}
 raw,_:=canonical.Marshal([]NegativeDeclinedDecision{entry})
 if out,err:=decodeNegativeDeclinedDecisions(raw,a,1);err!=nil||len(out)!=1 {t.Fatal("valid page",err)}
 if out,err:=decodeNegativeDeclinedDecisions([]byte("[]"),a,1);err!=nil||len(out)!=0 {t.Fatal("empty page",err)}
 for _,tc:=range []struct{name string;edit func(map[string]any)}{
  {"unknown",func(m map[string]any){m["authority"]=true}},
  {"missing origin",func(m map[string]any){delete(m,"origin_run_id")}},
  {"candidate route",func(m map[string]any){m["route"]="candidate"}},
  {"invalid digest",func(m map[string]any){m["decision_hash"]="invalid"}},
  {"cursor replay",func(m map[string]any){m["review_id"]=a.String()}},
  {"null",func(m map[string]any){m["origin_task_id"]=nil}},
 } {
  t.Run(tc.name,func(t *testing.T){var docs []map[string]any;_ = json.Unmarshal(raw,&docs);tc.edit(docs[0]);body,_:=json.Marshal(docs)
   if _,err:=decodeNegativeDeclinedDecisions(body,a,1);err==nil {t.Fatal("invalid decision accepted")}
  })
 }
 duplicate,_:=canonical.Marshal([]NegativeDeclinedDecision{entry,entry})
 if _,err:=decodeNegativeDeclinedDecisions(duplicate,a,2);err==nil {t.Fatal("duplicate page accepted")}
 entry.Review=a;low,_:=canonical.Marshal([]NegativeDeclinedDecision{entry})
 if _,err:=decodeNegativeDeclinedDecisions(low,b,1);err==nil {t.Fatal("backwards cursor accepted")}
 for _,body:=range []string{"null","{}","[null]","",string(duplicate)} {if _,err:=decodeNegativeDeclinedDecisions([]byte(body),a,1);err==nil {t.Fatal("malformed/oversized page accepted",body)}}
}
