package interchange

import (
 "encoding/json"
 "fmt"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
func TestCreateInterchangeTaggedUnionStrictDecode(t *testing.T){
 project,bundle,snapshot:=uuid.New(),uuid.New(),uuid.New();sha:="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 valid:=fmt.Sprintf(`{"project_id":%q,"direction":"import","format":"photoeditor_bundle","bundle":{"artifact_id":%q,"sha256":%q}}`,project.String(),bundle.String(),sha)
 export:=fmt.Sprintf(`{"project_id":%q,"direction":"export","format":"xmp","snapshot_id":%q}`,project.String(),snapshot.String())
 for _,raw:=range []string{valid,export}{var request CreateRequest;if err:=json.Unmarshal([]byte(raw),&request);err!=nil{t.Fatal(err)}}
 for _,raw:=range []string{
  valid[:len(valid)-1]+`,"snapshot_id":"`+snapshot.String()+`"}`,
  valid[:len(valid)-1]+`,"direction":"export"}`,
  `{"project_id":"`+project.String()+`","direction":"import","format":"xmp","bundle":null}`,
  `{"project_id":"`+project.String()+`","direction":"import","format":"xmp","bundle":{"artifact_id":"`+bundle.String()+`","sha256":"`+sha+`","sha256":"`+sha+`"}}`,
  export[:len(export)-1]+`,"unknown":1}`,
 } {var request CreateRequest;if json.Unmarshal([]byte(raw),&request)==nil{t.Fatal("invalid union accepted",raw)}}
}
func TestInterchangeIdentityOwnershipScopeAndSessionRotation(t *testing.T){
 tenant:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 request:=CreateRequest{ProjectID:uuid.New(),Direction:"import",Format:"photoeditor_bundle",Bundle:&ref}
 first,err:=FreezeCreateIntent(tenant,who,request,"retry-key");if err!=nil{t.Fatal(err)}
 who.SessionID=uuid.New();second,err:=FreezeCreateIntent(tenant,who,request,"retry-key");if err!=nil||first.Identity!=second.Identity||first.RequestSHA!=second.RequestSHA{t.Fatal("rotation changed identity",err)}
 ref.ArtifactID=uuid.New();if first.Request.Bundle.ArtifactID==ref.ArtifactID{t.Fatal("request not owned")}
 changed,err:=FreezeCreateIntent(tenant,who,request,"retry-key");if err!=nil||changed.Identity!=first.Identity||changed.RequestSHA==first.RequestSHA{t.Fatal("conflict must retain identity but change hash",err)}
 foreign,err:=FreezeCreateIntent(uuid.New(),who,request,"retry-key");if err!=nil||foreign.Identity.Run==first.Identity.Run{t.Fatal("tenant identity collision",err)}
}
