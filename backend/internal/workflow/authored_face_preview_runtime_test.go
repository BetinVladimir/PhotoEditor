package workflow

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2render"
)

func TestAuthoredFacePreviewCancellationBeforeDatabaseOrObjectIO(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,err:=(AuthoredFacePreviewRuntime{}).Prepare(ctx,"",v2render.AuthoredFaceSelectionCandidate{},nil)
 if !errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
}

func TestAuthoredFacePreviewDocumentRejectsForeignPhysicalCompletionBeforeSQL(t *testing.T){
 scope:=artifactjson.Scope{Tenant:uuid.New(),Run:uuid.New(),Owner:uuid.New(),Purpose:"task_params"}
 repository:=authoredFacePreviewDocumentRepository{scope:scope,params:`{"actual":"params"}`}
 ticket:=artifactjson.Reservation{ArtifactID:uuid.New(),Token:uuid.New()}
 key,err:=artifactjson.DocumentKey(scope,ticket.ArtifactID);if err!=nil{t.Fatal(err)}
 expected:=objectstore.Object{Key:key,VersionID:"exact-version",SHA256:renderDerivativeBytesSHA([]byte(repository.params)),Bytes:int64(len(repository.params)),ContentType:"application/json"}
 for _,name:=range []string{"tenant","run","owner","purpose","artifact","token","key","version_empty","version_null","sha","size","mime"}{
  t.Run(name,func(t *testing.T){
   s,tk,obj:=scope,ticket,expected
   switch name{case "tenant":s.Tenant=uuid.New();case "run":s.Run=uuid.New();case "owner":s.Owner=uuid.New();case "purpose":s.Purpose="input_manifest";case "artifact":tk.ArtifactID=uuid.Nil;case "token":tk.Token=uuid.Nil;case "key":obj.Key="tenants/foreign/object";case "version_empty":obj.VersionID="";case "version_null":obj.VersionID="null";case "sha":obj.SHA256=strings.Repeat("f",64);case "size":obj.Bytes++;case "mime":obj.ContentType="image/jpeg"}
   // Nil DB deliberately proves refusal occurs before a transaction or IO.
   if _,err:=repository.Complete(context.Background(),s,tk,obj);!errors.Is(err,objectstore.ErrInvalid){t.Fatal("foreign completion reached SQL or accepted",err)}
  })
 }
}

func TestAuthoredFacePreviewDocumentRejectsChangedBytesBeforeReservation(t *testing.T){
 scope:=artifactjson.Scope{Tenant:uuid.New(),Run:uuid.New(),Owner:uuid.New(),Purpose:"task_params"}
 repository:=authoredFacePreviewDocumentRepository{scope:scope,params:`{"actual":"params"}`}
 for _,name:=range []string{"sha","size","owner"}{t.Run(name,func(t *testing.T){
  s,sha,size:=scope,renderDerivativeBytesSHA([]byte(repository.params)),int64(len(repository.params))
  switch name{case "sha":sha=strings.Repeat("f",64);case "size":size++;case "owner":s.Owner=uuid.New()}
  if _,err:=repository.Reserve(context.Background(),s,sha,size);!errors.Is(err,objectstore.ErrInvalid){t.Fatal("changed immutable document reached SQL",err)}
 })}
}
