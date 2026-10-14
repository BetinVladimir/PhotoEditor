package maintenance

import (
 "bytes"
 "encoding/json"
 "errors"
 "io"
 "os"
 "path/filepath"
 "photoeditor/backend/internal/objectstore"
 "net/url"
 "strings"
 "syscall"
 "golang.org/x/sys/unix"
)

type tenantPurgeTarget struct {
 Endpoint string `json:"endpoint"`
 Bucket string `json:"bucket"`
 AccessKey string `json:"access_key"`
 SecretKey string `json:"secret_key"`
 Region string `json:"region"`
 Development *bool `json:"development"`
}
type tenantPurgeTargetManifest struct {
 SchemaVersion int `json:"schema_version"`
 Targets []tenantPurgeTarget `json:"targets"`
}

func secureTenantPurgeOwner(owner,effective uint32)bool{return owner==effective||owner==0}
func sameTenantPurgeTargetFile(after,current os.FileInfo)bool{
 return after!=nil&&current!=nil&&os.SameFile(after,current)&&current.Size()==after.Size()&&current.ModTime().Equal(after.ModTime())&&current.Mode()==after.Mode()
}
func openTenantPurgeTargetFile(path string)(*os.File,string,error){
 if !filepath.IsAbs(path)||filepath.Base(path)=="."||filepath.Base(path)=="/"{return nil,"",errors.New("insecure tenant purge target path")}
 parent,err:=filepath.EvalSymlinks(filepath.Dir(path));if err!=nil||!filepath.IsAbs(parent){return nil,"",errors.New("insecure tenant purge target path")}
 directory,err:=unix.Open("/",unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC,0);if err!=nil{return nil,"",errors.New("insecure tenant purge target path")}
 defer func(){_ = unix.Close(directory)}()
 for _,part:=range strings.Split(strings.TrimPrefix(parent,"/"),"/"){
  if part==""{continue}
  child,err:=unix.Openat(directory,part,unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,0)
  if err!=nil{return nil,"",errors.New("insecure tenant purge target path")}
  _ = unix.Close(directory);directory=child
  var info unix.Stat_t
  if err:=unix.Fstat(directory,&info);err!=nil||info.Mode&0022!=0||!secureTenantPurgeOwner(info.Uid,uint32(os.Geteuid())){return nil,"",errors.New("insecure tenant purge target directory")}
 }
 name:=filepath.Base(path)
 descriptor,err:=unix.Openat(directory,name,unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,0)
 if err!=nil{return nil,"",errors.New("insecure tenant purge target file")}
 canonical:=filepath.Join(parent,name)
 return os.NewFile(uintptr(descriptor),canonical),canonical,nil
}

// Additional targets are operator-owned credentials. They cannot be inferred
// from the primary writer configuration or a bucket name alone.
func loadTenantPurgeTargets(path string)([]tenantPurgeTarget,error){
 if path==""{return nil,nil}
 // Resolve system path aliases, then pin every canonical ancestor and refuse
 // symlink replacement or untrusted writable parents before opening secrets.
 file,canonical,err:=openTenantPurgeTargetFile(path)
 if err!=nil{return nil,err}
 defer file.Close()
 info,err:=file.Stat();if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{return nil,errors.New("insecure tenant purge target file")}
 stat,ok:=info.Sys().(*syscall.Stat_t)
 if !ok||stat.Nlink!=1||!secureTenantPurgeOwner(stat.Uid,uint32(os.Geteuid())){return nil,errors.New("insecure tenant purge target file")}
 raw,err:=io.ReadAll(io.LimitReader(file,1<<20+1));if err!=nil{return nil,err}
 after,err:=file.Stat();if err!=nil||!os.SameFile(info,after)||info.Size()!=after.Size()||!info.ModTime().Equal(after.ModTime())||after.Mode().Perm()&0077!=0{return nil,errors.New("tenant purge target file changed during read")}
 current,err:=os.Lstat(canonical)
 if err!=nil||!sameTenantPurgeTargetFile(after,current){return nil,errors.New("tenant purge target file replaced during read")}
 if len(raw)<2||len(raw)>1<<20{return nil,errors.New("invalid tenant purge targets")}
 unique:=json.NewDecoder(bytes.NewReader(raw))
 if uniqueTenantPurgeJSON(unique,0)!=nil{return nil,errors.New("invalid tenant purge targets")}
 if _,err=unique.Token();err!=io.EOF{return nil,errors.New("invalid tenant purge targets")}
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 var manifest tenantPurgeTargetManifest
 if decoder.Decode(&manifest)!=nil||manifest.SchemaVersion!=1||len(manifest.Targets)==0||len(manifest.Targets)>255{return nil,errors.New("invalid tenant purge targets")}
 var trailing any
 if decoder.Decode(&trailing)!=io.EOF{return nil,errors.New("invalid tenant purge targets")}
 return manifest.Targets,nil
}

func uniqueTenantPurgeJSON(decoder *json.Decoder,depth int)error{
 if depth>32{return errors.New("tenant purge target JSON too deep")}
 token,err:=decoder.Token();if err!=nil{return err}
 delimiter,ok:=token.(json.Delim);if !ok{return nil}
 switch delimiter{
 case '{':
  seen:=map[string]bool{}
  for decoder.More(){
   token,err=decoder.Token();if err!=nil{return err}
   key,ok:=token.(string);if !ok||seen[key]{return errors.New("duplicate tenant purge target key")}
   seen[key]=true
   if err=uniqueTenantPurgeJSON(decoder,depth+1);err!=nil{return err}
  }
  token,err=decoder.Token();if err!=nil||token!=json.Delim('}'){return errors.New("invalid tenant purge target object")}
 case '[':
  for decoder.More(){if err=uniqueTenantPurgeJSON(decoder,depth+1);err!=nil{return err}}
  token,err=decoder.Token();if err!=nil||token!=json.Delim(']'){return errors.New("invalid tenant purge target array")}
 default:return errors.New("invalid tenant purge target JSON")
 }
 return nil
}

func tenantPurgeConfigs(primary objectstore.Config,extra string,targets []tenantPurgeTarget)([]objectstore.Config,error){
 names,err:=tenantPurgeBucketNames(primary.Bucket,extra);if err!=nil{return nil,err}
 configs:=make([]objectstore.Config,0,len(names)+len(targets))
 seen:=map[string]bool{}
 appendConfig:=func(config objectstore.Config)error{
  endpoint,err:=url.Parse(config.Endpoint)
  if err!=nil||endpoint.Hostname()==""||endpoint.User!=nil||endpoint.Path!=""||endpoint.RawQuery!=""||endpoint.Fragment!=""||(endpoint.Scheme!="https"&&endpoint.Scheme!="http"){return errors.New("invalid tenant purge endpoint")}
  port:=endpoint.Port()
  if endpoint.Scheme=="https"&&port=="443"||endpoint.Scheme=="http"&&port=="80"{port=""}
  // A terminal DNS dot is an absolute-name spelling of the same endpoint.
  // Do not admit it as a second physical purge target with separate credentials.
  host:=strings.TrimSuffix(strings.ToLower(endpoint.Hostname()),".")
  identity:=endpoint.Scheme+"\x00"+host+"\x00"+port+"\x00"+config.Bucket
  if host==""||config.Bucket==""||seen[identity]||len(configs)>=256{return errors.New("invalid tenant purge target inventory")}
  seen[identity]=true;configs=append(configs,config);return nil
 }
 for _,name:=range names{config:=primary;config.Bucket=name;if err:=appendConfig(config);err!=nil{return nil,err}}
 for _,target:=range targets{
  if target.Endpoint==""||target.Bucket==""||target.AccessKey==""||target.SecretKey==""||target.Region==""||target.Development==nil{return nil,errors.New("incomplete tenant purge target")}
  config:=objectstore.Config{Endpoint:target.Endpoint,PublicEndpoint:target.Endpoint,Bucket:target.Bucket,AccessKey:target.AccessKey,SecretKey:target.SecretKey,Region:target.Region,Development:*target.Development}
  if err:=appendConfig(config);err!=nil{return nil,err}
 }
 return configs,nil
}
