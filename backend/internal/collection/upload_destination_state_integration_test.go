package collection

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/upload"
)

type destinationStateMultipart struct { upload.MultipartStore; begins int }
func(s *destinationStateMultipart)BeginMultipart(context.Context,string,string)(string,error){s.begins++;return "unexpected-multipart",nil}

// Authoring only: exercise the real upload service with photo_api and tenant
// RLS. Rejection must precede reservation, session and external storage writes.
func TestIntegrationUploadBeginRejectsClosedDestinationWithoutSideEffects(t *testing.T){
 ctx,admin,collections,who,tid,cid,_:=sectionAbandonFixture(t)
 var project uuid.UUID
 if err:=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,cid).Scan(&project);err!=nil{t.Fatal(err)}
 var beforeUploads,beforeReservations int64
 if err:=admin.QueryRow(ctx,`SELECT (SELECT count(*) FROM photo.upload_sessions WHERE tenant_id=$1),(SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=$1)`,tid).Scan(&beforeUploads,&beforeReservations);err!=nil{t.Fatal(err)}
 objects:=&destinationStateMultipart{}
 service:=upload.Service{DB:collections.DB,Objects:objects}
 for _,destination:=range []struct{state,code string}{{"archived","COLLECTION_ARCHIVED"},{"deleting","REVISION_CONFLICT"}}{
 if _,err:=admin.Exec(ctx,`UPDATE photo.collections SET state=$3 WHERE tenant_id=$1 AND id=$2`,tid,cid,destination.state);err!=nil{t.Fatal(err)}
 key:=uuid.NewString()
 for i:=0;i<2;i++{
  _,err:=service.Begin(ctx,who,tid,key,upload.BeginRequest{ProjectID:project,CollectionID:&cid,Filename:"photo.jpg",Bytes:100,Format:"jpeg"})
  var failure *domain.Error
  if !errors.As(err,&failure)||failure.Code!=destination.code{t.Fatalf("%s destination: expected %s, got %v",destination.state,destination.code,err)}
 }
 var uploads,reservations,receipts int64
 if err:=admin.QueryRow(ctx,`SELECT (SELECT count(*) FROM photo.upload_sessions WHERE tenant_id=$1),(SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=$1),(SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND route='Upload.Begin' AND key=$2)`,tid,key).Scan(&uploads,&reservations,&receipts);err!=nil{t.Fatal(err)}
 if uploads!=beforeUploads||reservations!=beforeReservations||receipts!=0||objects.begins!=0{t.Fatalf("rejected upload changed state: uploads=%d/%d reservations=%d/%d receipts=%d storage=%d",uploads,beforeUploads,reservations,beforeReservations,receipts,objects.begins)}
 }
}
