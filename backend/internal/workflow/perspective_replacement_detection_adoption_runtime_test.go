package workflow

import (
 "context"
 "strings"
 "testing"
 "time"
)

func replacementAdoptionRuntimeEnv()map[string]string{
 return map[string]string{
 "PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_ADOPTION_ENABLED":"true",
 "PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_PYTHON":"/usr/bin/python3",
 "PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_WORKER_ROOT":"/opt/photo/workers",
 "PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_BRIDGE_ROOT":"/tmp/photo-bridge",
 "DATABASE_URL":"postgresql://scheduler@localhost/photo","S3_ENDPOINT":"localhost:9000","S3_BUCKET":"photos","S3_ACCESS_KEY":"operator","S3_SECRET_KEY":"test-secret",
 "PHOTO_ICC_ROOT":"/opt/photo/icc","PHOTO_ADOPTER_PERSPECTIVE_LINES_ENABLED":"true",
 "PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG":"/opt/photo/detector.json","PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG_SHA":strings.Repeat("a",64),
 "PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_ROOT":"/tmp/photo-scratch","PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_MAX_BYTES":"1073741824","PHOTO_ADOPTER_PERSPECTIVE_LINES_RAM_MAX_BYTES":"2147483648",
 }
}
func TestReplacementAdoptionRuntimeConfiguration(t *testing.T){
 if _,_,err:=ReplacementDetectionAdoptionFromEnvironment(nil);err==nil{t.Fatal("nil environment")}
 if _,on,err:=ReplacementDetectionAdoptionFromEnvironment(func(string)string{return ""});on||err!=nil{t.Fatal(on,err)}
 env:=replacementAdoptionRuntimeEnv();env["UNRELATED_SECRET"]="must not be inherited"
 runner,on,err:=ReplacementDetectionAdoptionFromEnvironment(func(k string)string{return env[k]})
 if err!=nil||!on||runner.Timeout!=10*time.Minute||runner.Environment["UNRELATED_SECRET"]!=""{t.Fatal("valid explicit configuration rejected",err)}
 for _,key:=range []string{"PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_PYTHON","S3_SECRET_KEY","PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG_SHA","PHOTO_ADOPTER_PERSPECTIVE_LINES_ENABLED","PHOTO_ADOPTER_PERSPECTIVE_LINES_RAM_MAX_BYTES"}{
  t.Run(key,func(t *testing.T){invalid:=replacementAdoptionRuntimeEnv();invalid[key]="bad";if key=="S3_SECRET_KEY"{invalid[key]=""};if _,_,err:=ReplacementDetectionAdoptionFromEnvironment(func(k string)string{return invalid[k]});err==nil{t.Fatal("invalid config accepted")}})
 }
 for _,raw:=range []string{"0","601","bad"}{env["PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_TIMEOUT_SECONDS"]=raw;if _,_,err:=ReplacementDetectionAdoptionFromEnvironment(func(k string)string{return env[k]});err==nil{t.Fatal("invalid timeout accepted")}}
}
func TestReplacementAdoptionRuntimeRejectsMissingDependencies(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=NewReplacementDetectionAdoptionProcessor(ctx,nil,nil,"",PythonReplacementDetectionAdopter{});err!=context.Canceled{t.Fatal(err)}
 if _,err:=NewReplacementDetectionAdoptionProcessor(context.Background(),nil,nil,"photos",PythonReplacementDetectionAdopter{});err==nil{t.Fatal("missing dependencies accepted")}
}
