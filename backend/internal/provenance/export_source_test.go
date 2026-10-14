package provenance

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type signingCatalogStub struct{ready ReadySigningExport;calls int;change bool}
func(s *signingCatalogStub)LookupSigningExport(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(ReadySigningExport,error){s.calls++;value:=s.ready;if s.change&&s.calls>1{value.Object.VersionID="changed"};return value,nil}
type signingLineageStub struct{ref v2input.Ref;body []byte;calls int;ready ReadySigningExport}
func(s *signingLineageStub)ResolveFrozenExportLineage(_ context.Context,_ auth.Identity,r ReadySigningExport)(v2input.Ref,[]byte,error){s.calls++;s.ready=r;return s.ref,s.body,nil}
func TestBoundSigningSourcePinsCanonicalExportAndLineage(t *testing.T){
 sha:="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 for _,mode:=range []string{"valid","changed","foreign","mime","hash","lineage","cancelled"}{t.Run(mode,func(t *testing.T){
  tid,id:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
  catalog:=&signingCatalogStub{ready:ReadySigningExport{Tenant:tid,Project:uuid.New(),ExportID:id,Version:uuid.New(),Task:uuid.New(),Run:uuid.New(),Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},Object:objectstore.Object{Key:"encoded",VersionID:"v1",SHA256:sha,Bytes:100,ContentType:"image/jpeg"}}}
  body,ref:=contractBytes(t,SigningLineage{Schema:1,SourceHashes:[]string{sha},Actions:[]SigningAction{}})
  lineage:=&signingLineageStub{ref:ref,body:body};ctx:=context.Background()
  switch mode{case "changed":catalog.change=true;case "foreign":catalog.ready.Tenant=uuid.New();case "mime":catalog.ready.Object.ContentType="image/tiff";case "hash":catalog.ready.Object.SHA256="invalid";case "lineage":lineage.body=[]byte("{}");case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  result,err:=(BoundSigningExportSource{Catalog:catalog,Lineages:lineage}).ResolveSigningExport(ctx,who,tid,id)
  if mode=="valid"{
   if err!=nil||result.Export!=catalog.ready.Ref||catalog.calls!=2||lineage.ready!=catalog.ready{t.Fatal("source pins lost",err)}
   body[0]='x';if result.LineageBody[0]!='{'{t.Fatal("lineage bytes aliased")}
  }else if err==nil||result.Project!=uuid.Nil{t.Fatal("invalid source exposed")}
  if (mode=="foreign"||mode=="mime"||mode=="hash"||mode=="cancelled")&&lineage.calls!=0{t.Fatal("invalid export reached lineage")}
 })}
}
func TestSigningSQLCatalogCancellationAndMissingConfiguration(t *testing.T){
 if _,err:=(SQLSigningExportCatalog{}).LookupSigningExport(context.Background(),auth.Identity{},uuid.New(),uuid.New());err==nil{t.Fatal("missing dependencies accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(SQLSigningExportCatalog{}).LookupSigningExport(ctx,auth.Identity{},uuid.New(),uuid.New());err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
