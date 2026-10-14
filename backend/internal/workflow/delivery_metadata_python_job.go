package workflow

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
 "os/exec"
 "path/filepath"
 "regexp"
 "sort"
 "strconv"
 "strings"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type MetadataAdoptionCandidate struct{Tenant,Task,Result uuid.UUID;Attempt int;Fence int64}
func(c MetadataAdoptionCandidate)valid()bool{return c.Tenant!=uuid.Nil&&c.Task!=uuid.Nil&&c.Result!=uuid.Nil&&c.Attempt>=1&&c.Attempt<=2147483647&&c.Fence>=1&&c.Fence<=9007199254740991}
type PythonMetadataAdopter struct{Python,WorkerRoot string;Timeout time.Duration;Environment map[string]string}
var metadataAdoptionEnvironmentKeys=[]string{"DATABASE_URL","S3_ENDPOINT","S3_BUCKET","S3_ACCESS_KEY","S3_SECRET_KEY","APP_ENV",
 "PHOTO_METADATA_ADOPTION_SCRATCH_ROOT","PHOTO_METADATA_ADOPTION_MAX_BYTES","PHOTO_METADATA_ADOPTION_MAX_PIXELS","PHOTO_METADATA_ADOPTION_RAM_BYTES","PHOTO_METADATA_ADOPTION_SCRATCH_BYTES"}
func(p PythonMetadataAdopter)environment()([]string,error){
 if !filepath.IsAbs(p.Python)||filepath.Clean(p.Python)!=p.Python||!filepath.IsAbs(p.WorkerRoot)||filepath.Clean(p.WorkerRoot)!=p.WorkerRoot||
 p.Timeout<time.Second||p.Timeout>10*time.Minute||p.Timeout%time.Second!=0{return nil,v2input.ErrInvalid}
 allowed:=map[string]bool{};for _,key:=range metadataAdoptionEnvironmentKeys{allowed[key]=true;if key!="APP_ENV"&&p.Environment[key]==""{return nil,v2input.ErrInvalid}}
 keys:=make([]string,0,len(p.Environment))
 for key,value:=range p.Environment{if !allowed[key]||strings.ContainsRune(value,0){return nil,v2input.ErrInvalid};keys=append(keys,key)};sort.Strings(keys)
 root:=p.Environment["PHOTO_METADATA_ADOPTION_SCRATCH_ROOT"]
 if !filepath.IsAbs(root)||filepath.Clean(root)!=root||len(p.Environment["S3_BUCKET"])<3||len(p.Environment["S3_BUCKET"])>63{return nil,v2input.ErrInvalid}
 integer:=func(key string,maximum int64)(int64,error){
  raw:=p.Environment[key];if len(raw)==0||len(raw)>16{return 0,v2input.ErrInvalid};for _,c:=range raw{if c<'0'||c>'9'{return 0,v2input.ErrInvalid}}
  value,err:=strconv.ParseInt(raw,10,64);if err!=nil||value<1||value>maximum{return 0,v2input.ErrInvalid};return value,nil
 }
 size,err:=integer("PHOTO_METADATA_ADOPTION_MAX_BYTES",2<<30);if err!=nil{return nil,err}
 pixels,err:=integer("PHOTO_METADATA_ADOPTION_MAX_PIXELS",200000000);if err!=nil{return nil,err}
 ram,err:=integer("PHOTO_METADATA_ADOPTION_RAM_BYTES",9007199254740991);if err!=nil{return nil,err}
 scratch,err:=integer("PHOTO_METADATA_ADOPTION_SCRATCH_BYTES",9007199254740991);if err!=nil{return nil,err}
 if ram<(256<<20)+pixels*64+size*4||scratch<size*2+(8<<20){return nil,v2input.ErrInvalid}
 environment:=[]string{"PATH=/usr/bin:/bin","PYTHONPATH="+p.WorkerRoot,"PYTHONDONTWRITEBYTECODE=1","PYTHONNOUSERSITE=1","PYTHONHASHSEED=0","OMP_NUM_THREADS=1","OPENBLAS_NUM_THREADS=1"}
 for _,key:=range keys{environment=append(environment,key+"="+p.Environment[key])}
 return environment,nil
}
type metadataJobOutput struct{body []byte;cancel context.CancelFunc}
type MetadataJobRefusal struct{Code string}
func(e MetadataJobRefusal)Error()string{return e.Code}
var metadataJobCode=regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,79}$`)
func(o *metadataJobOutput)Write(raw []byte)(int,error){
 if len(raw)>4096-len(o.body){o.cancel();return 0,io.ErrShortBuffer}
 o.body=append(o.body,raw...);return len(raw),nil
}
func decodeMetadataJobResponse(raw []byte)error{
 var response struct{SchemaVersion int `json:"schema_version"`;Adopted bool `json:"adopted"`;ErrorCode *string `json:"error_code,omitempty"`}
 if len(raw)>4096||json.Unmarshal(raw,&response)!=nil{return v2input.ErrInvalid}
 exact,err:=canonical.Marshal(response)
 if err!=nil||!bytes.Equal(exact,raw)||response.SchemaVersion!=1{return v2input.ErrInvalid}
 if response.Adopted&&response.ErrorCode==nil{return nil}
 if !response.Adopted&&response.ErrorCode!=nil&&metadataJobCode.MatchString(*response.ErrorCode){return MetadataJobRefusal{Code:*response.ErrorCode}}
 return v2input.ErrInvalid
}
// Run reports a process result only. The dispatcher must independently read
// the SQL adoption receipt, including after timeout/unknown process completion.
func(p PythonMetadataAdopter)Run(ctx context.Context,c MetadataAdoptionCandidate)error{
 if err:=ctx.Err();err!=nil{return err};if !c.valid(){return v2input.ErrInvalid}
 environment,err:=p.environment();if err!=nil{return err}
 raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"tenant_id":c.Tenant,"task_id":c.Task,"attempt":c.Attempt,"fence":c.Fence,"result_id":c.Result,"timeout_seconds":int(p.Timeout/time.Second)})
 if err!=nil{return err}
 bounded,cancel:=context.WithTimeout(ctx,p.Timeout);defer cancel();output:=&metadataJobOutput{cancel:cancel}
 command:=exec.CommandContext(bounded,p.Python,"-m","photo_worker.v2.delivery_metadata_job")
 command.Dir=p.WorkerRoot;command.Env=environment;command.Stdin=bytes.NewReader(raw);command.Stdout=output;command.Stderr=io.Discard;command.WaitDelay=2*time.Second
 if err=command.Run();err!=nil{if bounded.Err()!=nil{return bounded.Err()};return err}
 if err=ctx.Err();err!=nil{return err}
 return decodeMetadataJobResponse(output.body)
}
