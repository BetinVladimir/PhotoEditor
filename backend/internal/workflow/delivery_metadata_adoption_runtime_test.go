package workflow

import (
 "context"
 "testing"
)
func TestMetadataAdoptionRuntimeExplicitOptIn(t *testing.T){
 if _,enabled,err:=MetadataAdoptionFromEnvironment(func(string)string{return ""});err!=nil||enabled{t.Fatal("default enabled",err)}
 if _,_,err:=MetadataAdoptionFromEnvironment(nil);err==nil{t.Fatal("nil environment")}
 env:=map[string]string{"PHOTO_DELIVERY_METADATA_ADOPTION_ENABLED":"true","PHOTO_DELIVERY_METADATA_ADOPTION_PYTHON":"/python",
 "PHOTO_DELIVERY_METADATA_ADOPTION_WORKER_ROOT":"/workers","DATABASE_URL":"database","S3_ENDPOINT":"https://objects","S3_BUCKET":"photos","S3_ACCESS_KEY":"access","S3_SECRET_KEY":"secret",
 "PHOTO_METADATA_ADOPTION_SCRATCH_ROOT":"/scratch","PHOTO_METADATA_ADOPTION_MAX_BYTES":"10000","PHOTO_METADATA_ADOPTION_MAX_PIXELS":"1000",
 "PHOTO_METADATA_ADOPTION_RAM_BYTES":"268539456","PHOTO_METADATA_ADOPTION_SCRATCH_BYTES":"8408608"}
 get:=func(key string)string{return env[key]}
 runner,enabled,err:=MetadataAdoptionFromEnvironment(get)
 if err!=nil||!enabled{t.Fatal(err)}
 for _,key:=range []string{"PHOTO_DELIVERY_METADATA_ADOPTION_ENABLED","PHOTO_DELIVERY_METADATA_ADOPTION_TIMEOUT_SECONDS","PHOTO_DELIVERY_METADATA_ADOPTION_PYTHON","PHOTO_METADATA_ADOPTION_RAM_BYTES"}{
  previous,exists:=env[key];env[key]="invalid"
  if _,_,err:=MetadataAdoptionFromEnvironment(get);err==nil{t.Fatal(key)}
  if exists{env[key]=previous}else{delete(env,key)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=NewMetadataAdoptionProcessor(ctx,nil,"photos",runner);err!=context.Canceled{t.Fatal(err)}
 if _,err:=NewMetadataAdoptionProcessor(context.Background(),nil,"photos",runner);err==nil{t.Fatal("missing DB")}
}
