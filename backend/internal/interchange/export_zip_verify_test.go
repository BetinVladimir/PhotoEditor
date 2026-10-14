package interchange
import (
 "archive/zip"
 "bytes"
 "context"
 "testing"
)
type exportZIPTestFile struct{path,body string;method uint16}
func exportZIPTestBytes(t *testing.T,files []exportZIPTestFile)[]byte{
 t.Helper();var output bytes.Buffer;writer:=zip.NewWriter(&output)
 for _,file:=range files{
  header:=&zip.FileHeader{Name:file.path,Method:file.method,ModifiedDate:33};header.SetMode(0600)
  target,err:=writer.CreateHeader(header);if err!=nil{t.Fatal(err)};if _,err:=target.Write([]byte(file.body));err!=nil{t.Fatal(err)}
 }
 if err:=writer.Close();err!=nil{t.Fatal(err)};return output.Bytes()
}
func TestExportZIPInventoryVerifiesMemberHashes(t *testing.T){
 raw:=exportZIPTestBytes(t,[]exportZIPTestFile{{"images/a.jpg","encoded-image",zip.Store},{"manifest.json",`{"schema_version":1}`,zip.Deflate}})
 files,err:=VerifyExportZIPInventory(context.Background(),bytes.NewReader(raw),int64(len(raw)),ExportLimits{1<<20,1<<20,10})
 if err!=nil||len(files)!=2||files[0].SHA256!=rawSHA([]byte("encoded-image"))||files[1].Path!="manifest.json"{t.Fatal("inventory",err)}
}
func TestExportZIPInventoryRejectsUnsafeStructureAndBudgets(t *testing.T){
 for _,kind:=range []string{"path","duplicate","order","method","manifest","expanded","files","physical","crc","directory","cancel"}{t.Run(kind,func(t *testing.T){
  entries:=[]exportZIPTestFile{{"images/a.jpg","encoded-image",zip.Store},{"manifest.json",`{"schema_version":1}`,zip.Deflate}}
  limits:=ExportLimits{1<<20,1<<20,10}
  switch kind{case "path":entries[0].path="../bad.jpg";case "duplicate":entries[1].path=entries[0].path;case "order":entries[0],entries[1]=entries[1],entries[0];case "method":entries[0].method=zip.Deflate;case "manifest":entries[1].path="other.json";case "expanded":limits.ExpandedBytes=2;case "files":limits.Files=1;case "physical":limits.ArchiveBytes=22}
  raw:=exportZIPTestBytes(t,entries)
  if kind=="crc"{index:=bytes.Index(raw,[]byte("encoded-image"));if index<0{t.Fatal("missing stored fixture")};raw[index]='x'}
  if kind=="directory"{raw[len(raw)-22]='x'}
  ctx:=context.Background();if kind=="cancel"{var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  if _,err:=VerifyExportZIPInventory(ctx,bytes.NewReader(raw),int64(len(raw)),limits);err==nil{t.Fatal("invalid ZIP accepted")}
 })}
}
