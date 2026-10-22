package interchange
import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestHistoricalImageGeometryNeverRewritesOriginal(t *testing.T){
 raw,err:=os.ReadFile("../review/testdata/working-images-python.json");if err!=nil{t.Fatal(err)}
 var images []json.RawMessage;if json.Unmarshal(raw,&images)!=nil||len(images)==0{t.Fatal("fixture")}
 original,err:=canonical.Bytes(images[0]);if err!=nil{t.Fatal(err)}
 if err:=validateImportHistoricalImageGeometry(original,original);err!=nil{t.Fatal(err)}
 var fields map[string]json.RawMessage;_ = json.Unmarshal(original,&fields)
 fields["color"]=json.RawMessage(`{"profile_id":"foreign","icc_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","encoding":"linear"}`)
 changed,_:=canonical.Marshal(fields)
 if validateImportHistoricalImageGeometry(original,changed)==nil{t.Fatal("foreign source color accepted as ancestry")}
}
func TestHistoricalImageAuthorityRequiresInstallation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=(SQLImportHistoricalImageAuthority{}).QualifyImportedHistoricalImage(ctx,[16]byte{},v2input.Ref{},nil,nil);err!=context.Canceled{t.Fatal("cancel lost",err)}
}
