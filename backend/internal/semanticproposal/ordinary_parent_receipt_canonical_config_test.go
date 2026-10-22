package semanticproposal

import (
 "encoding/json"
 "bytes"
 "context"
 "strings"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestOrdinaryReceiptCanonicalFractionAndNegativeConfig(t *testing.T){
 tenant,actor:=uuid.New(),uuid.New();who:=auth.Identity{UserID:actor,SessionID:uuid.New()};key:="canonical-receipt"
 run,err:=v2input.RunID(tenant,actor,key);if err!=nil{t.Fatal(err)};workflow,err:=v2input.WorkflowID(run);if err!=nil{t.Fatal(err)}
 // Real SQL jsonb output expands exponent notation; trusted RFC8785 must
 // reconstruct the original compiler hash without float64 map decoding.
 original:=json.RawMessage(`{"policy":{"offset":-1.25,"threshold":0.5,"small":1e-7}}`)
 stored:=json.RawMessage(`{"policy": {"small": 0.0000001, "threshold": 0.500, "offset": -1.250}}`)
 sha,err:=canonical.Hash(original);if err!=nil{t.Fatal(err)}
 if _,err:=canonical.Hash(json.RawMessage(`{"n":9007199254740992}`));err==nil{t.Fatal("unsafe integer canonicalization accepted independently of hash mismatch")}
 row:=map[string]any{"key":key,"request_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","run_id":run,"workflow_id":workflow,"config_snapshot":stored,"config_sha256":sha}
 encode:=func()[]byte{b,e:=json.Marshal(row);if e!=nil{t.Fatal(e)};return b}
 if got,k,found,e:=decodeOrdinaryParentReceipt(encode(),who,tenant,run);e!=nil||!found||k!=key||got.RunID!=run{t.Fatal("genuine canonical receipt",found,e)}
 for _,name:=range []string{"wrong-hash","changed-fraction","wrong-tenant","wrong-actor","wrong-parent","unsafe-integer","missing-snapshot","extra-field"}{
  t.Run(name,func(t *testing.T){
   copy:=map[string]any{};for k,v:=range row{copy[k]=v};tid,parent,identity:=tenant,run,who
   switch name{
   case "wrong-hash":copy["config_sha256"]="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
   case "changed-fraction":copy["config_snapshot"]=json.RawMessage(`{"policy":{"threshold":0.6}}`)
   case "wrong-tenant":tid=uuid.New()
   case "wrong-actor":identity.UserID=uuid.New()
   case "wrong-parent":parent=uuid.New()
   case "unsafe-integer":copy["config_snapshot"]=json.RawMessage(`{"n":9007199254740992}`)
   case "missing-snapshot":delete(copy,"config_snapshot")
   case "extra-field":copy["foreign"]=true
   }
   raw,e:=json.Marshal(copy);if e!=nil{t.Fatal(e)}
   if _,_,found,e:=decodeOrdinaryParentReceipt(raw,identity,tid,parent);e==nil||found{t.Fatal("hostile receipt accepted")}
  })
 }
}

// Called by the actual existing scene-tail fixture only after genuine accepted
// receipt. Uses the authorized SQL response; never manufactures saved state.
func assertOrdinaryReceiptCanonicalBoundary(t *testing.T,ctx context.Context,reader SQLTransferSourceReader,who auth.Identity,tid,parent uuid.UUID,requireFraction bool){
 t.Helper();var raw []byte
 err:=tenant.WithTenant(ctx,reader.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,"SELECT photo.ordinary_parent_accepted_receipt_locator($1,$2,$3,$4,$5)",tid,reader.ProjectID,who.UserID,who.SessionID,parent).Scan(&raw)
 });if err!=nil{t.Fatal("genuine accepted locator response",err)}
 if _,_,found,e:=decodeOrdinaryParentReceipt(raw,who,tid,parent);e!=nil||!found{t.Fatal("actual canonical accepted receipt",e)}
 var fields map[string]json.RawMessage;if err=json.Unmarshal(raw,&fields);err!=nil{t.Fatal(err)}
 if requireFraction{
  d:=json.NewDecoder(bytes.NewReader(fields["config_snapshot"]));d.UseNumber();var config any;if err=d.Decode(&config);err!=nil{t.Fatal(err)}
  var fractional func(any)bool
  fractional=func(v any)bool{switch x:=v.(type){case json.Number:return strings.ContainsAny(string(x),".eE");case map[string]any:for _,child:=range x{if fractional(child){return true}};case []any:for _,child:=range x{if fractional(child){return true}}};return false}
  if !fractional(config){t.Fatal("actual scene config must exercise noninteger lexical serializer boundary")}
 }
 // Mutate only an owned response copy after the genuine SQL positive, never
 // the installed row, trigger, policy, qualification, or producer.
 fields["config_sha256"]=json.RawMessage(`"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`)
 changed,e:=json.Marshal(fields);if e!=nil{t.Fatal(e)}
 if _,_,found,e:=decodeOrdinaryParentReceipt(changed,who,tid,parent);e==nil||found{t.Fatal("actual response wrong hash accepted")}
 if _,_,found,e:=decodeOrdinaryParentReceipt(raw,who,uuid.New(),parent);e==nil||found{t.Fatal("actual response foreign tenant accepted")}
 t.Log("ORDINARY_RECEIPT_REAL_CANONICAL_CONFIG_AND_HOSTILES_OK")
}
