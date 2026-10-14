package interchange

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type backgroundPublishCatalog struct{object objectstore.Object;calls int;change bool}
func(s *backgroundPublishCatalog)LookupImportDocument(_ context.Context,node v2input.Entry)(objectstore.Object,error){s.calls++;if node.Contract!="Artifact"{return objectstore.Object{},v2input.ErrInvalid};out:=s.object;if s.change&&s.calls>1{out.VersionID="changed"};return out,nil}
type backgroundPublishNative struct{calls int;failure error}
func(s *backgroundPublishNative)QualifyImportBackgroundRaster(_ context.Context,_ v2input.Ref,_ objectstore.Object)(string,error){s.calls++;return "image/png",s.failure}

type backgroundPublishAuthority struct{calls int;revoke bool}
func(s *backgroundPublishAuthority)QualifyImportBackgroundTarget(context.Context,v2input.SessionVersionBinding)error{s.calls++;if s.revoke&&s.calls>1{return v2input.ErrInvalid};return nil}
type backgroundPublishCommit struct{calls int;wrong bool}
func(s *backgroundPublishCommit)CommitImportedBackground(_ context.Context,_ ImportBackgroundSource,_ objectstore.Object,_ v2input.SessionVersionBinding,id,media string)(string,error){s.calls++;if s.wrong{return "wrong",nil};return id,nil}
func TestQualifiedBackgroundPublicationRefusesChangedAuthorityAndUnqualifiedBytes(t *testing.T){
 for _,mode:=range []string{"valid","native_failure","changed_source","revoked_target","wrong_receipt","media","size","cancelled"}{t.Run(mode,func(t *testing.T){
  sha:=strings.Repeat("a",64);source:=ImportBackgroundSource{Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},Staged:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}}
  catalog:=&backgroundPublishCatalog{object:objectstore.Object{Key:"staged",VersionID:"v1",SHA256:sha,Bytes:100,ContentType:"application/octet-stream"}}
  native:=&backgroundPublishNative{};authority:=&backgroundPublishAuthority{};commit:=&backgroundPublishCommit{}
  switch mode{case "native_failure":native.failure=errors.New("invalid raster");case "changed_source":catalog.change=true;case "revoked_target":authority.revoke=true;case "wrong_receipt":commit.wrong=true;case "media":catalog.object.ContentType="image/png";case "size":catalog.object.Bytes=536870913}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  publisher:=QualifiedImportBackgroundPublisher{Catalog:catalog,Native:native,Authority:authority,Committer:commit}
  id,err:=publisher.PublishImportedBackground(ctx,source,v2input.SessionVersionBinding{},"local-bg")
  if mode=="valid"{if err!=nil||id!="local-bg"||commit.calls!=1||native.calls!=1||authority.calls!=2||catalog.calls!=2{t.Fatal("missing qualification lifecycle",err)};return}
  if err==nil||id!=""{t.Fatal("accepted invalid publication")}
  if mode!="wrong_receipt"&&commit.calls!=0{t.Fatal("committed before qualification")}
 })}
}
