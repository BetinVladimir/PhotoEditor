package delivery

import (
 "testing"
 "context"
 "errors"
 "github.com/google/uuid"
 "time"
)
func TestMetadataSourceEnvironmentConfiguration(t *testing.T){
 env:=map[string]string{}
 get:=func(key string)string{return env[key]}
 if _,enabled,err:=MetadataSourceFromEnvironment(get);err!=nil||enabled{t.Fatal(enabled,err)}
 if _,_,err:=MetadataSourceFromEnvironment(nil);err==nil{t.Fatal("nil environment")}
 env["PHOTO_DELIVERY_METADATA_SOURCE_ENABLED"]="yes"
 if _,_,err:=MetadataSourceFromEnvironment(get);err==nil{t.Fatal("invalid flag")}
 env=map[string]string{"PHOTO_DELIVERY_METADATA_SOURCE_ENABLED":"true","PHOTO_DELIVERY_METADATA_SOURCE_PYTHON":"/usr/bin/python3","PHOTO_DELIVERY_METADATA_SOURCE_WORKER_ROOT":"/workers","DATABASE_URL":"postgres://api","S3_ENDPOINT":"localhost:9000","S3_BUCKET":"photos","S3_ACCESS_KEY":"key","S3_SECRET_KEY":"secret","PHOTO_METADATA_SOURCE_SCRATCH_ROOT":"/scratch","PHOTO_METADATA_SOURCE_MAX_BYTES":"1024","PHOTO_METADATA_SOURCE_RAM_BYTES":"268439552","PHOTO_METADATA_SOURCE_SCRATCH_BYTES":"8389632"}
 runner,enabled,err:=MetadataSourceFromEnvironment(get)
 if err!=nil||!enabled||runner.Timeout!=600*time.Second{t.Fatal(enabled,err)}
 env["S3_SECRET_KEY"]="changed"
 if runner.Environment["S3_SECRET_KEY"]!="secret"{t.Fatal("environment not owned")}
 for _,raw:=range []string{"0","601","+1","1.0"," 1","-1"}{
  env["PHOTO_DELIVERY_METADATA_SOURCE_TIMEOUT_SECONDS"]=raw
  if _,_,err:=MetadataSourceFromEnvironment(get);err==nil{t.Fatal("invalid timeout",raw)}
 }
 env["PHOTO_DELIVERY_METADATA_SOURCE_TIMEOUT_SECONDS"]="1"
 if _,enabled,err:=MetadataSourceFromEnvironment(get);err!=nil||!enabled{t.Fatal(enabled,err)}
 delete(env,"DATABASE_URL")
 if _,_,err:=MetadataSourceFromEnvironment(get);err==nil{t.Fatal("missing API credentials")}
}

func TestMetadataSourceDependencyInstallationPreflight(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=NewMetadataSourceDependencies(ctx,nil,uuid.Nil,"",nil,PythonMetadataSourcePreparer{});!errors.Is(err,context.Canceled){t.Fatal(err)}
 if _,err:=NewMetadataSourceDependencies(context.Background(),nil,uuid.New(),"photos",nil,PythonMetadataSourcePreparer{});err==nil{t.Fatal("missing dependencies accepted")}
}

func TestMetadataSourceDedicatedDatabaseCredentials(t *testing.T){
 env:=map[string]string{"PHOTO_DELIVERY_METADATA_SOURCE_ENABLED":"true","PHOTO_DELIVERY_METADATA_SOURCE_PYTHON":"/usr/bin/python3","PHOTO_DELIVERY_METADATA_SOURCE_WORKER_ROOT":"/workers","DATABASE_URL":"postgres://general","PHOTO_DELIVERY_METADATA_SOURCE_DATABASE_URL":"postgres://metadata-api","S3_ENDPOINT":"localhost:9000","S3_BUCKET":"photos","S3_ACCESS_KEY":"key","S3_SECRET_KEY":"secret","PHOTO_METADATA_SOURCE_SCRATCH_ROOT":"/scratch","PHOTO_METADATA_SOURCE_MAX_BYTES":"1024","PHOTO_METADATA_SOURCE_RAM_BYTES":"268439552","PHOTO_METADATA_SOURCE_SCRATCH_BYTES":"8389632"}
 get:=func(key string)string{return env[key]}
 for _,mode:=range []string{"override","dedicated_only"}{
  if mode=="dedicated_only"{delete(env,"DATABASE_URL")}
  runner,enabled,err:=MetadataSourceFromEnvironment(get)
  if err!=nil||!enabled||runner.Environment["DATABASE_URL"]!="postgres://metadata-api"{t.Fatal(mode,enabled,err)}
  if _,exists:=runner.Environment["PHOTO_DELIVERY_METADATA_SOURCE_DATABASE_URL"];exists{t.Fatal("parent key leaked to child environment")}
 }
 env["PHOTO_DELIVERY_METADATA_SOURCE_DATABASE_URL"]="postgres://invalid\x00dsn"
 if _,_,err:=MetadataSourceFromEnvironment(get);err==nil{t.Fatal("invalid dedicated DSN accepted")}
 env["PHOTO_DELIVERY_METADATA_SOURCE_ENABLED"]="false"
 if _,enabled,err:=MetadataSourceFromEnvironment(get);err!=nil||enabled{t.Fatal("disabled installation consumed credentials",enabled,err)}
}
