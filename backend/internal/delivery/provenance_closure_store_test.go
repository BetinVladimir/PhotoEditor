package delivery

import (
 "context"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestRetainVerifiedSigningClosureRequiresCompleteLeaseAndReceipt(t *testing.T){
 sha:=strings.Repeat("a",64)
 lease:=ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now().Add(time.Minute),
  Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),ParentTask:uuid.New(),Cursor:1,SlotCount:1}
 actor,policy,preset,exportID:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 export:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},CanvasHash:sha}
 slot:=Slot{Ordinal:0,Binding:binding,PresetID:preset,PresetSHA:sha,Required:true,State:"ready",ExportID:&exportID,Export:&export}
 dependencies,err:=CompileSigningDependencies(lease.Run,policy,[]Slot{slot});if err!=nil{t.Fatal(err)}
 workflow:=uuid.NewSHA1(lease.Tenant,[]byte("provenance:"+actor.String()+":"+dependencies[0].IdempotencyKey))
 task:=uuid.NewSHA1(workflow,[]byte("provenance.sign:0"))
 signed:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha};manifest:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 receipt:=SignedDependencyReceipt{Signed:signed,Manifest:manifest,ValidationSHA:sha,TaskID:task,ResultID:uuid.New(),Bucket:"artifacts",
  SignedObject:objectstore.Object{Key:"tenants/a/signed",VersionID:"v1",SHA256:sha,Bytes:42,ContentType:"image/jpeg"},
  ManifestObject:objectstore.Object{Key:"tenants/a/manifest",VersionID:"v2",SHA256:sha,Bytes:13,ContentType:"application/c2pa"}}
 for _,mode:=range []string{"accepted","stale","missing_receipt","wrong_cursor","canceled"}{
  current:=lease;currentReceipts:=[]SignedDependencyReceipt{receipt};ctx,cancel:=context.WithCancel(context.Background())
  db:=&assemblyRetainDB{accepted:mode!="stale"}
  switch mode{
  case "missing_receipt":currentReceipts=nil
  case "wrong_cursor":current.Cursor=0
  case "canceled":cancel()
  }
  closure,err:=(ExpansionLeaseStore{DB:db}).RetainVerifiedSigningClosure(ctx,current,actor,policy,sha,
   []Slot{slot},dependencies,currentReceipts)
  cancel()
  if mode=="accepted" {if err!=nil||len(closure.Entries)!=1||db.calls!=1||len(db.args)!=4{t.Fatal(mode,err)}}
  if mode=="stale" {if err==nil||db.calls!=1{t.Fatal(mode,err)}}
  if mode!="accepted"&&mode!="stale" {if err==nil||db.calls!=0{t.Fatal(mode,err)}}
 }
 _,raw,closureSHA,err:=CompileSignedAssemblyClosureForSlots(lease.Tenant,actor,lease.Run,policy,sha,
  []Slot{slot},dependencies,[]SignedDependencyReceipt{receipt});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"accepted","missing","wrong_sha","changed_receipt","wrong_cursor"}{
  db:=&signedClosureReadDB{raw:string(raw),sha:closureSHA}
  current:=lease;currentReceipt:=receipt
  switch mode{
  case "missing":db.err=pgx.ErrNoRows
  case "wrong_sha":db.sha=strings.Repeat("b",64)
  case "changed_receipt":currentReceipt.ResultID=uuid.New()
  case "wrong_cursor":current.Cursor=0
  }
  got,err:=(ExpansionLeaseStore{DB:db}).ReadRetainedSigningClosure(context.Background(),current,
   actor,policy,sha,[]Slot{slot},dependencies,[]SignedDependencyReceipt{currentReceipt})
  if mode=="accepted" {if err!=nil||len(got.Entries)!=1||db.calls!=1{t.Fatal(mode,err)}}
  if mode!="accepted" {if err==nil{t.Fatal("accepted",mode)};if mode=="wrong_cursor"&&db.calls!=0{t.Fatal("invalid cursor reached SQL")}}
 }
}

type signedClosureReadDB struct{raw,sha string;err error;calls int}
func(d *signedClosureReadDB)QueryRow(context.Context,string,...any)pgx.Row{d.calls++;return signedClosureReadRow{d.raw,d.sha,d.err}}
type signedClosureReadRow struct{raw,sha string;err error}
func(r signedClosureReadRow)Scan(dest ...any)error{if r.err!=nil{return r.err};*dest[0].(*string)=r.raw;*dest[1].(*string)=r.sha;return nil}
