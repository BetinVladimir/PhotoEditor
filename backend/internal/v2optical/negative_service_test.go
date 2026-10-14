package v2optical

import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestNegativeIntentClosedSchemaAndOwnedParams(t *testing.T) {
 params,_,_,_:=negativeDraftFixture(t)
 original:=NegativeIntent{SchemaVersion:1,Snapshot:uuid.New(),Params:params}
 raw,err:=canonical.Marshal(original);if err!=nil { t.Fatal(err) }
 got,err:=DecodeNegativeIntent(raw);if err!=nil || got!=original { t.Fatal("valid intent",err) }
 for _,fault:=range []string{"unknown","policy","snapshot","version","params"} {
  var doc map[string]any;json.Unmarshal(raw,&doc)
  switch fault {
  case "unknown":doc["source_jcs"]="{}"
  case "policy":doc["qualification"]=map[string]any{}
  case "snapshot":doc["snapshot_id"]=uuid.Nil.String()
  case "version":doc["schema_version"]=2
  case "params":doc["params"].(map[string]any)["strength"]=2
  }
  changed,_:=canonical.Marshal(doc)
  if _,err:=DecodeNegativeIntent(changed);err==nil { t.Fatal("invalid intent accepted",fault) }
 }
}
func TestNegativeUnconfiguredProjectCannotSubmit(t *testing.T) {
 p:=&NegativeProjects{services:map[[2]uuid.UUID]*NegativeService{}}
 if _,err:=p.SubmitProjectIntent(context.Background(),auth.Identity{},uuid.New(),uuid.New(),"key",NegativeIntent{});err==nil { t.Fatal("unconfigured project accepted") }
 var s *NegativeService
 if _,err:=s.SubmitProjectIntent(context.Background(),auth.Identity{},uuid.New(),uuid.New(),"key",NegativeIntent{});err!=v2input.ErrInvalid { t.Fatal("nil service",err) }
}
