package maintenance

import (
 "bytes"
 "encoding/json"
 "os"
 "path/filepath"
 "photoeditor/backend/internal/objectstore"
 "strings"
 "syscall"
 "testing"
)

func TestTenantPurgeTargetsBindDistinctEndpointsAndCredentials(t *testing.T){
 primary:=objectstore.Config{Endpoint:"https://main.example",Bucket:"photos",AccessKey:"main-key",SecretKey:"main-secret",Region:"us-east-1"}
 disabled:=false
 targets:=[]tenantPurgeTarget{{Endpoint:"https://archive.example",Bucket:"photos",AccessKey:"archive-key",SecretKey:"archive-secret",Region:"eu-west-1",Development:&disabled}}
 configs,err:=tenantPurgeConfigs(primary,"exports",targets)
 if err!=nil||len(configs)!=3||configs[0].Bucket!="photos"||configs[1].Bucket!="exports"||configs[2].Endpoint!=targets[0].Endpoint||configs[2].AccessKey!=targets[0].AccessKey||configs[2].SecretKey!=targets[0].SecretKey||configs[2].Region!=targets[0].Region{t.Fatal("tenant purge target scope lost",err)}
 if _,err:=tenantPurgeConfigs(primary,"",[]tenantPurgeTarget{{Endpoint:primary.Endpoint,Bucket:primary.Bucket,AccessKey:"other",SecretKey:"other",Region:"eu-west-1",Development:&disabled}});err==nil{t.Fatal("duplicate physical bucket accepted")}
 if _,err:=tenantPurgeConfigs(primary,"",[]tenantPurgeTarget{{Endpoint:"https://MAIN.example:443",Bucket:primary.Bucket,AccessKey:"other",SecretKey:"other",Region:"eu-west-1",Development:&disabled}});err==nil{t.Fatal("endpoint alias duplicated primary bucket")}
 if _,err:=tenantPurgeConfigs(primary,"",[]tenantPurgeTarget{{Endpoint:"https://MAIN.example.:443",Bucket:primary.Bucket,AccessKey:"other",SecretKey:"other",Region:"eu-west-1",Development:&disabled}});err==nil{t.Fatal("absolute DNS alias duplicated primary bucket")}
 for _,target:=range []tenantPurgeTarget{{Endpoint:"https://archive.example",Bucket:"photos",AccessKey:"",SecretKey:"secret",Region:"eu-west-1",Development:&disabled},{Endpoint:"",Bucket:"photos",AccessKey:"key",SecretKey:"secret",Region:"eu-west-1",Development:&disabled},{Endpoint:"https://archive.example",Bucket:"photos",AccessKey:"key",SecretKey:"secret",Region:"eu-west-1"}}{
  if _,err:=tenantPurgeConfigs(primary,"",[]tenantPurgeTarget{target});err==nil{t.Fatal("incomplete target accepted")}
 }
}

func TestTenantPurgeTargetManifestClosedAndBounded(t *testing.T){
 write:=func(raw string)string{t.Helper();path:=filepath.Join(t.TempDir(),"targets.json");if err:=os.WriteFile(path,[]byte(raw),0600);err!=nil{t.Fatal(err)};return path}
 valid:=`{"schema_version":1,"targets":[{"endpoint":"https://archive.example","bucket":"photos","access_key":"key","secret_key":"secret","region":"eu-west-1","development":false}]}`
 got,err:=loadTenantPurgeTargets(write(valid));if err!=nil||len(got)!=1||got[0].Endpoint!="https://archive.example"{t.Fatal("valid purge targets refused",err)}
 insecure:=write(valid)
 if err:=os.Chmod(insecure,0644);err!=nil{t.Fatal(err)}
 if _,err:=loadTenantPurgeTargets(insecure);err==nil{t.Fatal("world-readable purge credentials accepted")}
 regular:=write(valid)
 symlink:=filepath.Join(t.TempDir(),"targets-link.json")
 if err:=os.Symlink(regular,symlink);err!=nil{t.Fatal(err)}
 if _,err:=loadTenantPurgeTargets(symlink);err==nil{t.Fatal("symlinked purge credentials accepted")}
 hardlink:=filepath.Join(t.TempDir(),"targets-hardlink.json")
 if err:=os.Link(regular,hardlink);err!=nil{t.Fatal(err)}
 if _,err:=loadTenantPurgeTargets(regular);err==nil{t.Fatal("multiply linked purge credentials accepted")}
 fifo:=filepath.Join(t.TempDir(),"targets-fifo.json")
 if err:=syscall.Mkfifo(fifo,0600);err!=nil{t.Fatal(err)}
 if _,err:=loadTenantPurgeTargets(fifo);err==nil{t.Fatal("FIFO purge credentials accepted")}
 for _,raw:=range []string{
  `{"schema_version":1,"schema_version":1,"targets":[]}`,
  strings.Replace(valid,`"bucket":"photos"`,`"bucket":"photos","bucket":"other"`,1),
  strings.Replace(valid,`"region":"eu-west-1"`,`"region":"eu-west-1","unknown":true`,1),
  strings.Replace(valid,`"schema_version":1`,`"schema_version":2`,1),
  strings.Repeat("x",1<<20+1),
 }{
  if _,err:=loadTenantPurgeTargets(write(raw));err==nil{t.Fatal("invalid purge manifest accepted")}
 }
}

func TestTenantPurgeTargetJSONDepthBound(t *testing.T){
 raw:=strings.Repeat("[",34)+"0"+strings.Repeat("]",34)
 if err:=uniqueTenantPurgeJSON(json.NewDecoder(bytes.NewBufferString(raw)),0);err==nil{t.Fatal("deep target JSON accepted")}
}

func TestTenantPurgeTargetFileOwnerPolicy(t *testing.T){
 for _,item:=range []struct{owner,effective uint32;allowed bool}{
  {1001,1001,true},{0,1001,true},{1002,1001,false},{1002,0,false},
 }{
  if secureTenantPurgeOwner(item.owner,item.effective)!=item.allowed{t.Fatal("tenant purge credential owner policy",item)}
 }
}

func TestTenantPurgeTargetPathRecheckDetectsInPlaceMutation(t *testing.T){
 path:=filepath.Join(t.TempDir(),"targets.json")
 if err:=os.WriteFile(path,[]byte("original"),0600);err!=nil{t.Fatal(err)}
 before,err:=os.Stat(path);if err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,[]byte("changed-content"),0600);err!=nil{t.Fatal(err)}
 current,err:=os.Lstat(path);if err!=nil{t.Fatal(err)}
 if !os.SameFile(before,current)||sameTenantPurgeTargetFile(before,current){t.Fatal("same-inode mutation passed target identity check")}
}

func TestTenantPurgeTargetRequiresTrustedParentsAndPinnedFinalFile(t *testing.T){
 valid:=`{"schema_version":1,"targets":[{"endpoint":"https://archive.example","bucket":"photos","access_key":"key","secret_key":"secret","region":"eu-west-1","development":false}]}`
 root:=t.TempDir();secure:=filepath.Join(root,"secure")
 if err:=os.Mkdir(secure,0700);err!=nil{t.Fatal(err)}
 path:=filepath.Join(secure,"targets.json")
 if err:=os.WriteFile(path,[]byte(valid),0600);err!=nil{t.Fatal(err)}
 if _,err:=loadTenantPurgeTargets("secure/targets.json");err==nil{t.Fatal("relative credential path accepted")}
 alias:=filepath.Join(root,"alias")
 if err:=os.Symlink(secure,alias);err!=nil{t.Fatal(err)}
 if targets,err:=loadTenantPurgeTargets(filepath.Join(alias,"targets.json"));err!=nil||len(targets)!=1{t.Fatal("canonical trusted parent refused",err)}
 if err:=os.Chmod(secure,0777);err!=nil{t.Fatal(err)}
 if _,err:=loadTenantPurgeTargets(path);err==nil{t.Fatal("writable credential parent accepted")}
 if err:=os.Chmod(secure,0700);err!=nil{t.Fatal(err)}
 if err:=os.Remove(path);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(filepath.Join(root,"missing"),path);err!=nil{t.Fatal(err)}
 if _,err:=loadTenantPurgeTargets(path);err==nil{t.Fatal("final credential symlink accepted")}
}
