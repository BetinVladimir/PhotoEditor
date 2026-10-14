package provenance

import (
 "bytes"
 "context"
 "io"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type signingObjectsStub struct{body []byte;opens int}
func(s *signingObjectsStub)Open(context.Context,objectstore.Object)(io.ReadCloser,error){s.opens++;return io.NopCloser(bytes.NewReader(s.body)),nil}
func TestSigningScratchOwnsBoundedVerifiedFiles(t *testing.T){
 for _,mode:=range []string{"valid","hash","short","extra","version","budget","cancelled"}{t.Run(mode,func(t *testing.T){
  raw:=[]byte("encoded bytes");sha:=bodyDigest(raw)
  catalog:=&signingCatalogStub{ready:ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),ExportID:uuid.New(),Version:uuid.New(),Task:uuid.New(),Run:uuid.New(),Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},Object:objectstore.Object{Key:"encoded",VersionID:"v1",SHA256:sha,Bytes:int64(len(raw)),ContentType:"image/jpeg"}}}
  objects:=&signingObjectsStub{body:raw};maximum:=int64(100);ctx:=context.Background()
  switch mode{case "hash":objects.body=[]byte("changed bytes");case "short":objects.body=raw[:2];case "extra":objects.body=append(append([]byte(nil),raw...),byte('x'));case "version":catalog.change=true;case "budget":maximum=1;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  root:=t.TempDir();scratch,err:=NewSigningScratch(root,maximum);if err!=nil{t.Fatal(err)}
  staged,err:=scratch.Stage(ctx,auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},catalog.ready,catalog,objects)
  if mode=="valid"{
   if err!=nil||scratch.used!=int64(len(raw)){t.Fatal("valid staging rejected",err)}
   stored,err:=os.ReadFile(staged.Path());if err!=nil||!bytes.Equal(stored,raw){t.Fatal("staged bytes changed",err)}
   if err:=staged.Close();err!=nil{t.Fatal(err)};if err:=staged.Close();err!=nil{t.Fatal(err)}
   if scratch.used!=0{t.Fatal("scratch budget not released")}
  }else if err==nil||staged!=nil{t.Fatal("invalid staging accepted")}
  if mode!="valid"{entries,err:=os.ReadDir(root);if err!=nil||len(entries)!=0||scratch.used!=0{t.Fatal("failure leaked scratch",err)}}
  if (mode=="budget"||mode=="cancelled")&&objects.opens!=0{t.Fatal("invalid staging reached object IO")}
 })}
}
