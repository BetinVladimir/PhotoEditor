package workflow

import (
 "context"
 "errors"
 "io"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
)
func TestMetadataPythonJobAdmissionAndBoundedOutput(t *testing.T){
 runner:=PythonMetadataAdopter{Python:"/python",WorkerRoot:"/workers",Timeout:time.Minute,Environment:map[string]string{
 "DATABASE_URL":"database","S3_ENDPOINT":"https://objects","S3_BUCKET":"photos","S3_ACCESS_KEY":"access","S3_SECRET_KEY":"secret",
 "PHOTO_METADATA_ADOPTION_SCRATCH_ROOT":"/scratch","PHOTO_METADATA_ADOPTION_MAX_BYTES":"10000","PHOTO_METADATA_ADOPTION_MAX_PIXELS":"1000",
 "PHOTO_METADATA_ADOPTION_RAM_BYTES":"268539456","PHOTO_METADATA_ADOPTION_SCRATCH_BYTES":"8408608"}}
 if _,err:=runner.environment();err!=nil{t.Fatal(err)}
 for _,key:=range []string{"PYTHONPATH","LD_PRELOAD","PHOTO_METADATA_ADOPTION_RAM_BYTES"}{
  values:=map[string]string{};for k,v:=range runner.Environment{values[k]=v};values[key]="1";current:=runner;current.Environment=values
  if _,err:=current.environment();err==nil{t.Fatal(key)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=runner.Run(ctx,MetadataAdoptionCandidate{Tenant:uuid.New(),Task:uuid.New(),Result:uuid.New(),Attempt:1,Fence:1});err!=context.Canceled{t.Fatal(err)}
 if err:=decodeMetadataJobResponse([]byte(`{"adopted":true,"schema_version":1}`));err!=nil{t.Fatal(err)}
 refused:=decodeMetadataJobResponse([]byte(`{"adopted":false,"error_code":"DELIVERY_METADATA_PHYSICAL_INVALID","schema_version":1}`))
 var semantic MetadataJobRefusal
 if !errors.As(refused,&semantic)||semantic.Code!="DELIVERY_METADATA_PHYSICAL_INVALID"{t.Fatal("semantic refusal lost",refused)}
 for _,raw:=range []string{`{"adopted":false,"schema_version":1}`,`{"adopted":true,"schema_version":1,"extra":1}`,`{"adopted":true,"error_code":"INVALID","schema_version":1}`,`{"adopted":false,"error_code":"secret detail","schema_version":1}`,`{"adopted":false,"error_code":"INVALID","schema_version":1,"extra":1}`,`{ "adopted":true,"schema_version":1}`,`{}`} {
  if err:=decodeMetadataJobResponse([]byte(raw));err==nil{t.Fatal(raw)}
 }
 ctx,cancel=context.WithCancel(context.Background());defer cancel();output:=metadataJobOutput{cancel:cancel}
 if _,err:=output.Write([]byte(strings.Repeat("x",4096)));err!=nil{t.Fatal(err)}
 if _,err:=output.Write([]byte("x"));err!=io.ErrShortBuffer||ctx.Err()!=context.Canceled{t.Fatal("stdout bound",err)}
}
