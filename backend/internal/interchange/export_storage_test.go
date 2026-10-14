package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type exportStorageStoreStub struct{reservation artifactjson.Reservation;reserve,complete int}
func(s *exportStorageStoreStub)ReserveExport(_ context.Context,_ CreateIdentity,_ CreateDocument)(artifactjson.Reservation,error){s.reserve++;return s.reservation,nil}
func(s *exportStorageStoreStub)CompleteExport(_ context.Context,_ CreateIdentity,doc CreateDocument,_ artifactjson.Reservation,_ objectstore.Object)(v2input.Ref,error){s.complete++;return doc.Ref,nil}
func TestExportStoragePublishesAndReplaysExactDocuments(t *testing.T){
 for _,replay:=range []bool{false,true}{
  intent,compiled,_,_:=exportFreezeFixture(t);ref,body:=compiled.PlanDocument()
  store:=&exportStorageStoreStub{reservation:artifactjson.Reservation{ArtifactID:ref.ArtifactID,Token:uuid.New()}}
  if replay{store.reservation.Ready=&artifactjson.Ref{ID:ref.ArtifactID,SHA256:ref.SHA256}}
  objects:=&createStorageObjectsStub{}
  publisher:=ExportStoragePublisher{Tenant:uuid.New(),Store:store,Objects:objects}
  result,err:=publisher.PublishExportDocument(context.Background(),intent.Identity,CreateDocument{Purpose:"v30_interchange_export_plan",Ref:ref,Body:body})
  if err!=nil||result!=ref{t.Fatal("publication",err)}
  if replay{if objects.put!=0||store.complete!=0{t.Fatal("replay did IO")}}else if objects.put!=1||store.complete!=1{t.Fatal("missing publication IO")}
 }
}
func TestExportStorageRejectsWrongPurposeIdentityAndObjectReceipt(t *testing.T){
 for _,mode:=range []string{"purpose","identity","body","object"}{t.Run(mode,func(t *testing.T){
  intent,compiled,_,_:=exportFreezeFixture(t);ref,body:=compiled.PlanDocument()
  doc:=CreateDocument{Purpose:"v30_interchange_export_plan",Ref:ref,Body:body}
  switch mode{case "purpose":doc.Purpose="v30_interchange_snapshot";case "identity":doc.Ref.ArtifactID=uuid.New();case "body":doc.Body=[]byte(`{}`)}
  store:=&exportStorageStoreStub{reservation:artifactjson.Reservation{ArtifactID:ref.ArtifactID,Token:uuid.New()}}
  objects:=&createStorageObjectsStub{corrupt:mode=="object"}
  publisher:=ExportStoragePublisher{Tenant:uuid.New(),Store:store,Objects:objects}
  if _,err:=publisher.PublishExportDocument(context.Background(),intent.Identity,doc);err==nil{t.Fatal("invalid publication accepted")}
  if mode!="object"&&store.reserve!=0{t.Fatal("invalid document reached store")}
  if store.complete!=0{t.Fatal("invalid publication completed")}
 })}
}
