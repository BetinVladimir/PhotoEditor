package collection

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
)

func TestCreationAbandonWireHasOnlyItsActualParent(t *testing.T){
 parent:=uuid.New();key:=uuid.NewString()
 for _,scenario:=range []struct{route,parent,foreign string}{{"Share.Create","collection_id","project_id"},{"Collection.Create","project_id","collection_id"}}{
  t.Run(scenario.route,func(t *testing.T){
   body,err:=creationAbandonBody(scenario.route,key,parent);if err!=nil{t.Fatal(err)}
   var fields map[string]json.RawMessage
   if err=json.Unmarshal(body,&fields);err!=nil{t.Fatal(err)}
   if len(fields)!=3||fields[scenario.foreign]!=nil{t.Fatalf("abandon receipt exposed unrelated parent: %s",body)}
   var gotStatus,gotKey,gotParent string
   if err=json.Unmarshal(fields["status"],&gotStatus);err!=nil{t.Fatal(err)}
   if err=json.Unmarshal(fields["key"],&gotKey);err!=nil{t.Fatal(err)}
   if err=json.Unmarshal(fields[scenario.parent],&gotParent);err!=nil{t.Fatal(err)}
   if gotStatus!="abandoned"||gotKey!=key||gotParent!=parent.String(){t.Fatalf("wrong abandon identity: %s",body)}
  })
 }
}
