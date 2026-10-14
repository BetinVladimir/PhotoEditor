package delivery
import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
)
func TestWatermarkCatalogObservationRetainsScopeAndHash(t *testing.T){
 intent,who,tid,ordinary:=createCatalogFixture(t)
 var wire map[string]json.RawMessage;if json.Unmarshal(ordinary,&wire)!=nil{t.Fatal("fixture")}
 _,config,_:=watermarkPinFixture(t)
 id:=intent.request.Policy.RequiredPresets[0]
 presets,err:=json.Marshal([]any{map[string]any{"id":id,"revision":"1","sha256":digest(config),"config":json.RawMessage(config)}});if err!=nil{t.Fatal(err)}
 wire["presets"]=presets;raw,err:=json.Marshal(wire);if err!=nil{t.Fatal(err)}
 tx:=&deliveryReadTx{raw:raw}
 catalog,err:=intent.ResolveWatermarkCatalog(context.Background(),tx,who,tid);if err!=nil||catalog.Presets[0].SHA256!=digest(config)||len(tx.args)!=6||tx.args[0]!=tid||tx.args[3]!=who.UserID{t.Fatal("catalog",err)}
 if _,err:=intent.DecodeCatalog(raw);err==nil{t.Fatal("ordinary path accepted watermark")}
 if _,err:=intent.ResolveWatermarkCatalog(context.Background(),tx,who,uuid.New());err==nil{t.Fatal("foreign tenant")}
 var changed []map[string]json.RawMessage;if json.Unmarshal(presets,&changed)!=nil{t.Fatal("fixture")};changed[0]["sha256"]=json.RawMessage(`"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`);wire["presets"],_=json.Marshal(changed);bad,_:=json.Marshal(wire)
 if _,err:=intent.DecodeWatermarkCatalog(bad);err==nil{t.Fatal("hash substitution")}
 ctx,cancel:=context.WithCancel(context.Background());cancel();if _,err:=intent.ResolveWatermarkCatalog(ctx,tx,who,tid);err!=context.Canceled{t.Fatal("cancel",err)}
}
