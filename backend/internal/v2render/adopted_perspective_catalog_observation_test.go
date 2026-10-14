package v2render
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type perspectiveCatalogObservation struct {
 *previewBundleFixture
 binding AdoptedPerspectiveBinding
 reads int
 changed bool
}
func(f *perspectiveCatalogObservation) LoadObject(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID,v2input.Ref,string)(AdoptedPerspectiveBinding,objectstore.Object,error){
 f.reads++
 b:=f.binding
 if f.changed&&f.reads>2{b.Base.Source.ArtifactID=uuid.NewSHA1(b.Base.Source.ArtifactID,[]byte("changed"))}
 raw:=f.docs[b.Result.ArtifactID.String()]
 return b,objectstore.Object{Key:b.Result.ArtifactID.String(),VersionID:"immutable",SHA256:b.Result.SHA256,Bytes:int64(len(raw)),ContentType:"application/json"},nil
}
func(f *perspectiveCatalogObservation) LoadChildObject(_ context.Context,_ auth.Identity,_,_,_,_ uuid.UUID,_ v2input.Ref,child v2input.Ref,_ string,contract,_ string)(objectstore.Object,error){
 media,_,ok:=perspectiveChildObjectLimit(contract);if !ok{return objectstore.Object{},v2input.ErrInvalid}
 size:=int64(len(f.docs[child.ArtifactID.String()]));if size==0&&media!="application/json"{size=224}
 return objectstore.Object{Key:child.ArtifactID.String(),VersionID:"immutable",SHA256:child.SHA256,Bytes:size,ContentType:media},nil
}
func TestPerspectiveCatalogRefusesBindingChangedDuringDescendantIO(t *testing.T){
 for _,changed:=range []bool{false,true}{
  fixture:=newPreviewBundleFixture(t)
  f:=&perspectiveCatalogObservation{previewBundleFixture:fixture,binding:AdoptedPerspectiveBinding{ResultID:fixture.pin.ResultID,Result:fixture.pin.Result,Base:fixture.pin.Base},changed:changed}
  ref:=fixture.pin.Base.Source
  original:=&perspectiveFieldCatalog{object:objectstore.Object{Key:ref.ArtifactID.String(),VersionID:"immutable",SHA256:ref.SHA256,Bytes:int64(len(f.docs[ref.ArtifactID.String()])),ContentType:"application/json"}}
  catalog,_,err:=buildAdoptedPerspectiveCatalog(context.Background(),f,original,f,fixture.pin.Principal,fixture.pin.Tenant,fixture.pin.Project,fixture.pin.Base.AssetVersionID,fixture.pin.Producer,fixture.pin.Result,"photos")
  if changed {
   if err!=v2input.ErrExpired||catalog!=nil{t.Fatal("changed final binding exposed",err)}
  }else if err!=nil||catalog==nil||f.reads!=4{t.Fatal("complete catalog observation missing",err,f.reads)}
 }
}
