package v2optical

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestOpticalDraftInventoryDigestBindsOperationAndTask(t *testing.T){
 task:=uuid.New()
 entries:=[]v2input.Entry{{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
 seen:=map[string]bool{}
 for _,op:=range []string{"optical.negative","optical.perspective","optical.panorama","optical.focus_stack"}{
  got,err:=opticalDraftInputDigest(task,op,entries);if err!=nil{t.Fatal(err)}
  expected,err:=canonical.Hash(map[string]any{"task":task.String(),"operation":op,"version":"1","entries":entries});if err!=nil{t.Fatal(err)}
  if got!=expected||seen[got]{t.Fatalf("operation binding lost for %s",op)};seen[got]=true
  other,err:=opticalDraftInputDigest(uuid.New(),op,entries);if err!=nil||other==got{t.Fatal("task binding lost",err)}
 }
 old,err:=negativeDraftInputDigest(task,entries);if err!=nil{t.Fatal(err)}
 current,err:=opticalDraftInputDigest(task,"optical.negative",entries);if err!=nil||old!=current{t.Fatal("negative namespace changed",err)}
}

func TestOpticalDraftInventoryDigestRejectsUnknownIdentity(t *testing.T){
 for _,op:=range []string{"","optical.unknown","provenance.sign"}{
  if _,err:=opticalDraftInputDigest(uuid.New(),op,nil);err==nil{t.Fatal("unknown operation accepted")}
 }
 if _,err:=opticalDraftInputDigest(uuid.Nil,"optical.perspective",nil);err==nil{t.Fatal("nil task accepted")}
}
