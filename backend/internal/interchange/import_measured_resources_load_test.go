package interchange
import (
 "context"
 "os"
 "path/filepath"
 "testing"
 "photoeditor/backend/internal/canonical"
)
func TestImportMeasurementLoaderRequiresPinnedPrivateRegularFile(t *testing.T){
 digest:=func(s string)string{return rawSHA([]byte(s))}
 report:=ImportResourceMeasurements{Schema:"interchange-import-resource-measurements/v1",Runtime:digest("runtime"),Contracts:digest("contracts"),Hardware:digest("hardware"),Measurements:[]ImportResourceMeasurement{{DocumentsSHA:digest("documents"),InputDigest:digest("inputs"),RAM:1<<30,Scratch:0,Output:1}}}
 raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","digest","symlink","mutable","relative","cancelled"}{t.Run(mode,func(t *testing.T){
  root:=t.TempDir();path:=filepath.Join(root,"measurements.json");if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)}
  pin:=rawSHA(raw);ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{
  case "digest":pin=digest("other")
  case "symlink":link:=path+".link";if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)};path=link
  case "mutable":if err:=os.Chmod(path,0666);err!=nil{t.Fatal(err)}
  case "relative":path="measurements.json"
  case "cancelled":cancel()
  }
  q,err:=LoadMeasuredImportResourceQualifier(ctx,path,pin,report.Runtime,report.Contracts,report.Hardware)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected measurement file admission",err)}
  if mode!="valid"&&q!=nil{t.Fatal("invalid file returned qualifier")}
 })}
}
