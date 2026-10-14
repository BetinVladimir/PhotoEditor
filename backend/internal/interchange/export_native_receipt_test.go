package interchange
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestExportNativeReceiptBindsExactObjectAndQualifiedRuntime(t *testing.T){
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))};entry:=v2input.Entry{Ref:ref,Contract:"EncodedImage"};object:=objectstore.Object{SHA256:ref.SHA256,Bytes:5,ContentType:"image/tiff"};runtime:=rawSHA([]byte("qualified-runtime"))
 base:=func()map[string]any{return map[string]any{"schema_version":1,"sha256":ref.SHA256,"bytes":5,"media_type":"image/tiff","width":100,"height":100,"bit_depth":16,"has_alpha":false,"orientation":6,"icc_sha256":nil,"runtime_sha256":runtime}}
 for _,mode:=range []string{"valid","sha","size","runtime","pixels","orientation","depth","missing_alpha","missing_icc","extra","icc","noncanonical"}{t.Run(mode,func(t *testing.T){
  value:=base();switch mode{case "sha":value["sha256"]=runtime;case "size":value["bytes"]=6;case "runtime":value["runtime_sha256"]=ref.SHA256;case "pixels":value["width"]=200000000;case "orientation":value["orientation"]=9;case "depth":value["bit_depth"]=32;case "missing_alpha":delete(value,"has_alpha");case "missing_icc":delete(value,"icc_sha256");case "extra":value["approved"]=true;case "icc":value["icc_sha256"]="bad"}
  raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)};if mode=="noncanonical"{raw=append(raw,' ')}
  receipt,err:=decodeExportNativeImageReceipt(raw,entry,object,runtime)
  if mode=="valid"{if err!=nil||receipt.Width!=100||receipt.Alpha==nil||*receipt.Alpha{t.Fatal("valid receipt",err)}}else if err==nil||receipt.Schema!=0{t.Fatal("invalid native receipt exposed",mode,err)}
 })}
}
