package workflow

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
 "os/exec"
 "path/filepath"
 "sort"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type PythonReplacementDetectionAdopter struct{
 Python,WorkerRoot,BridgeRoot string
 Timeout time.Duration
 // Explicit operator environment; never inherited from the parent process.
 Environment map[string]string
}
type replacementDetectionJobOutput struct{body []byte;cancel context.CancelFunc}
func(o *replacementDetectionJobOutput) Write(raw []byte)(int,error){
 if len(raw)>4096-len(o.body){o.cancel();return 0,io.ErrShortBuffer};o.body=append(o.body,raw...);return len(raw),nil
}
func(p PythonReplacementDetectionAdopter) Run(ctx context.Context,r ReplacementDetectionDocuments,attempt int,fence int64,result uuid.UUID,root v2input.Ref)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if !filepath.IsAbs(p.Python)||!filepath.IsAbs(p.WorkerRoot)||p.Timeout<time.Second||p.Timeout>10*time.Minute||attempt<1||attempt>6||fence<1||fence>9007199254740991||result==uuid.Nil||root.ArtifactID==uuid.Nil||domainDigestInvalid(root.SHA256){return false,v2input.ErrInvalid}
 scope,_,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return false,err}
 allowed:=map[string]bool{}
 for _,key:=range []string{"DATABASE_URL","S3_ENDPOINT","S3_BUCKET","S3_ACCESS_KEY","S3_SECRET_KEY","APP_ENV","PHOTO_ICC_ROOT","PHOTO_ADOPTER_PERSPECTIVE_LINES_ENABLED","PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG","PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG_SHA","PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_ROOT","PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_MAX_BYTES","PHOTO_ADOPTER_PERSPECTIVE_LINES_RAM_MAX_BYTES"}{allowed[key]=true}
 environment:=[]string{"PATH=/usr/bin:/bin","PYTHONPATH="+p.WorkerRoot,"PYTHONDONTWRITEBYTECODE=1","PYTHONNOUSERSITE=1","PYTHONHASHSEED=0","OMP_NUM_THREADS=1","OPENBLAS_NUM_THREADS=1"}
 keys:=make([]string,0,len(p.Environment));for key:=range p.Environment{if !allowed[key]{return false,v2input.ErrInvalid};keys=append(keys,key)};sort.Strings(keys)
 for _,key:=range keys{environment=append(environment,key+"="+p.Environment[key])}
 for _,key:=range []string{"DATABASE_URL","S3_ENDPOINT","S3_BUCKET","S3_ACCESS_KEY","S3_SECRET_KEY","PHOTO_ICC_ROOT","PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG","PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG_SHA","PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_ROOT","PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_MAX_BYTES","PHOTO_ADOPTER_PERSPECTIVE_LINES_RAM_MAX_BYTES"}{if p.Environment[key]==""{return false,v2input.ErrInvalid}}
 if p.Environment["PHOTO_ADOPTER_PERSPECTIVE_LINES_ENABLED"]!="true"||p.Environment["S3_BUCKET"]!=r.Bucket{return false,v2input.ErrInvalid}
 recovered:=false
 err=(ReplacementDetectionBridgeJob{Root:p.BridgeRoot,Timeout:p.Timeout}).Run(ctx,r,func(ctx context.Context,socket,token string)error{
  raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"socket":socket,"token":token,"tenant_id":scope.Tenant,"run_id":scope.Run,"task_id":scope.Owner,
   "descriptor_jcs":string(r.Plan.Descriptor()),"attempt":attempt,"fence":fence,"result_id":result,"root":root,"params":task.Params,"timeout_seconds":int(p.Timeout/time.Second)})
  if err!=nil{return err};if len(raw)>4<<20{return v2input.ErrInvalid}
  bounded,cancel:=context.WithCancel(ctx);defer cancel()
  output:=&replacementDetectionJobOutput{cancel:cancel}
  command:=exec.CommandContext(bounded,p.Python,"-m","photo_worker.v2.perspective_replacement_detection_job")
  command.Dir=p.WorkerRoot;command.Env=environment;command.Stdin=bytes.NewReader(raw);command.Stdout=output;command.Stderr=io.Discard;command.WaitDelay=2*time.Second
  if err=command.Run();err!=nil{if bounded.Err()!=nil{return bounded.Err()};return err}
  var response struct{SchemaVersion int `json:"schema_version"`;Recovered bool `json:"recovered_existing"`}
  if json.Unmarshal(output.body,&response)!=nil{return v2input.ErrInvalid}
  exact,err:=canonical.Marshal(response);if err!=nil||!bytes.Equal(exact,output.body)||response.SchemaVersion!=1{return v2input.ErrInvalid}
  recovered=response.Recovered;return ctx.Err()
 });if err!=nil{return false,err};return recovered,nil
}
