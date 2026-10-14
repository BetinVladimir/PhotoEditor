package review

import (
 "bytes"
 "encoding/json"
 "testing"

 "github.com/google/uuid"
)

func TestReviewViewPublishesDecisionIdentityOnlyWhenPresent(t *testing.T){
 hash:="abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
 decided:=View{ID:uuid.New(),RunID:uuid.New(),Kind:"render.compose",State:"decided",Revision:2,DecisionHash:&hash,Reasons:json.RawMessage(`[]`)}
 encoded,err:=json.Marshal(decided);if err!=nil{t.Fatal(err)}
 if !bytes.Contains(encoded,[]byte(`"decision_hash":"`+hash+`"`)){t.Fatal("decided review lost decision identity")}
 pending:=decided;pending.State="pending";pending.DecisionHash=nil
 encoded,err=json.Marshal(pending);if err!=nil{t.Fatal(err)}
 if bytes.Contains(encoded,[]byte(`"decision_hash"`)){t.Fatal("pending review advertised a decision identity")}
}
