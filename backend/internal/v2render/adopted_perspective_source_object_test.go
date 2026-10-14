package v2render
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
func TestPerspectiveSourceCatalogRefusesMissingAuthorityAndCancellation(t *testing.T){
 c:=perspectiveSourceCatalog{}
 node:=v2draft.LayerRef{Contract:"WorkingImage",Path:"Original"}
 if _,err:=c.Lookup(context.Background(),auth.Identity{},uuid.New(),node);err!=v2input.ErrInvalid{t.Fatal("missing source authority accepted",err)}
 node.Contract="DenseWarpField"
 if _,err:=c.Lookup(context.Background(),auth.Identity{},uuid.New(),node);err!=v2input.ErrInvalid{t.Fatal("unscoped dense field accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=c.Lookup(ctx,auth.Identity{},uuid.New(),node);err!=context.Canceled{t.Fatal("cancellation ignored",err)}
}

func TestPerspectiveSourceCatalogRejectsUndeclaredFieldBeforeDatabase(t *testing.T) {
 c:=&perspectiveSourceCatalog{sourceRaw:[]byte(`{}`),sourceRef:v2input.Ref{ArtifactID:uuid.New()}}
 node:=v2draft.LayerRef{Contract:"DenseWarpField",Path:"Document.transform_chain.steps[0].inverse_field",Ref:v2input.Ref{ArtifactID:uuid.New()}}
 if _,err:=c.Lookup(context.Background(),auth.Identity{},uuid.New(),node);err==nil{t.Fatal("undeclared field accepted")}
 var absent *perspectiveSourceCatalog
 if _,err:=absent.Lookup(context.Background(),auth.Identity{},uuid.New(),node);err!=v2input.ErrInvalid{t.Fatal("nil authority accepted",err)}
}

type perspectiveInputSourceDouble struct {
 calls int
 parent v2input.Ref
 path string
 document []byte
 failure error
}
func(d *perspectiveInputSourceDouble) LoadSourceObject(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID,v2input.Ref,v2input.Ref,string)(objectstore.Object,error){
 return objectstore.Object{},v2input.ErrInvalid
}
func(d *perspectiveInputSourceDouble) LoadSourceField(_ context.Context,_ auth.Identity,_,_,_,_ uuid.UUID,_ v2input.Ref,parent,field v2input.Ref,document []byte,path,_ string)(objectstore.Object,error){
 d.calls++;d.parent=parent;d.path=path;d.document=append([]byte(nil),document...)
 return objectstore.Object{Key:"field",VersionID:"immutable",SHA256:field.SHA256,Bytes:224,ContentType:"application/x-npy"},d.failure
}
func TestPerspectiveSourceCatalogRechecksExactDenseOccurrence(t *testing.T){
 raw,parent,field:=originalDenseFixture(t,false)
 fields,err:=perspectiveOriginalDenseFields(raw,parent);if err!=nil{t.Fatal(err)}
 node:=fields[field.ArtifactID]
 authority:=&perspectiveInputSourceDouble{}
 fallback:=&perspectiveFieldCatalog{}
 c:=&perspectiveSourceCatalog{source:authority,fallback:fallback,sourceRef:parent,sourceRaw:append([]byte(nil),raw...)}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant:=uuid.New()
 if _,err:=resolvePerspectiveOriginalField(context.Background(),c,who,tenant,node);err!=nil{t.Fatal(err)}
 if authority.calls!=1||authority.parent!=parent||authority.path!=node.Path||string(authority.document)!=string(raw)||len(fallback.seen)!=0{t.Fatal("actual-source authority bypassed")}
 for _,mutate:=range []func(*v2draft.LayerRef){
  func(n *v2draft.LayerRef){n.Path="Document.transform_chain.steps[1].inverse_field"},
  func(n *v2draft.LayerRef){n.ParentRef.ArtifactID=uuid.New()},
  func(n *v2draft.LayerRef){n.Ref.ArtifactID=uuid.New()},
  func(n *v2draft.LayerRef){n.CanvasHash="unrelated"},
 }{
  bad:=node;mutate(&bad)
  if _,err:=c.Lookup(context.Background(),who,tenant,bad);err!=v2input.ErrInvalid{t.Fatal("altered occurrence accepted",err)}
 }
 if authority.calls!=1||len(fallback.seen)!=0{t.Fatal("invalid occurrence reached authority or fallback")}
 authority.failure=v2input.ErrExpired
 if _,err:=resolvePerspectiveOriginalField(context.Background(),c,who,tenant,node);err!=v2input.ErrExpired{t.Fatal("revocation hidden by cache",err)}
 if authority.calls!=2||len(fallback.seen)!=0{t.Fatal("revocation skipped current authority")}
}
