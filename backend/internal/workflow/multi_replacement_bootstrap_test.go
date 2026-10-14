package workflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "strings"
 "testing"
)

func TestMultiReplacementBootstrapRefusesMissingAuthority(t *testing.T){
 for _,operation:=range []string{"optical.panorama","optical.focus_stack"}{
  stem,_:=multiReplacementConfigStem(operation)
  t.Setenv(stem,"")
  projects,err:=buildMultiReplacementProjects(context.Background(),nil,nil,"bucket",operation)
  if err!=nil||projects!=nil{t.Fatalf("optional deployment: %v",err)}
  t.Setenv(stem,"/not-opened-without-dependencies")
  if _,err=buildMultiReplacementProjects(context.Background(),nil,nil,"bucket",operation);err==nil{t.Fatal("missing authority accepted")}
 }
 if err:=verifyMultiReplacementInstallation(context.Background(),nil);err==nil{t.Fatal("missing database accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=verifyMultiReplacementInstallation(ctx,nil);err!=context.Canceled{t.Fatal(err)}
 if _,err:=buildMultiReplacementProjects(ctx,nil,nil,"bucket","optical.panorama");err!=context.Canceled{t.Fatal(err)}
 if _,err:=buildMultiReplacementProjects(context.Background(),nil,nil,"bucket","optical.perspective");err==nil{t.Fatal("unsupported operation accepted")}
}

func TestMultiSchedulerDeploymentRejectsUnpinnedOrMalformedConfiguration(t *testing.T){
 raw:=[]byte(`{"schema_version":1,"projects":[]}`)
 sum:=sha256.Sum256(raw);pin:=hex.EncodeToString(sum[:])
 for _,operation:=range []string{"optical.panorama","optical.focus_stack"}{
  for _,candidate:=range []string{"",strings.Repeat("0",64),strings.ToUpper(pin),pin}{
   if _,err:=decodeMultiSchedulerDeployment(operation,raw,candidate);err==nil{t.Fatal("unqualified empty deployment accepted")}
  }
  if _,err:=decodeMultiSchedulerDeployment(operation,make([]byte,(8<<20)+1),pin);err==nil{t.Fatal("oversized deployment accepted")}
 }
 if _,err:=decodeMultiSchedulerDeployment("optical.perspective",raw,pin);err==nil{t.Fatal("unsupported operation accepted")}
}

func TestMultiReplacementRequiresNegativeDeployment(t *testing.T){
 t.Setenv("PHOTO_V29_NEGATIVE_CONFIG","")
 t.Setenv("PHOTO_V29_PERSPECTIVE_CONFIG","")
 t.Setenv("PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_PREPARATION_ENABLED","")
 for _,stem:=range []string{"PHOTO_V29_PANORAMA_CONFIG","PHOTO_V29_FOCUS_STACK_CONFIG"}{
  t.Setenv(stem,"configured")
  if _,err:=buildNegativeReplacementScheduler(context.Background(),nil,nil,"bucket");err==nil{t.Fatal("orphan multi replacement deployment accepted")}
  t.Setenv(stem,"")
 }
}
