package delivery

import (
 "context"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestSignedDependencyReceiptExactIdentity(t *testing.T){
 sha:=strings.Repeat("a",64)
 dependency:=SigningDependency{ExportID:uuid.New(),Export:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},PolicyID:uuid.New()}
 receipt:=SignedDependencyReceipt{Signed:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},Manifest:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},ValidationSHA:sha,TaskID:uuid.New(),ResultID:uuid.New()}
	receipt.Bucket="photos"
	receipt.SignedObject=v2SignedObject(receipt.TaskID,receipt.Signed,"image/jpeg",10)
	receipt.ManifestObject=v2SignedObject(receipt.TaskID,receipt.Manifest,"application/c2pa",10)
 if !validSignedDependencyReceipt(dependency,receipt){t.Fatal("valid signing receipt refused")}
 for _,mode:=range []string{"unsigned_alias","manifest_alias","missing_digest","missing_task","missing_result"}{
  changed:=receipt
  switch mode{
  case "unsigned_alias":changed.Signed.ArtifactID=dependency.Export.ArtifactID
  case "manifest_alias":changed.Manifest.ArtifactID=receipt.Signed.ArtifactID
  case "missing_digest":changed.ValidationSHA=""
  case "missing_task":changed.TaskID=uuid.Nil
  case "missing_result":changed.ResultID=uuid.Nil
  }
  if validSignedDependencyReceipt(dependency,changed){t.Fatal("invalid receipt accepted",mode)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,_,err:=ReadSignedDependencyReceipt(ctx,nil,auth.Identity{},uuid.Nil,uuid.Nil,dependency,sha);err!=context.Canceled{t.Fatal("cancelled receipt reached DB",err)}
 if _,_,err:=ReadSignedDependencyReceipt(context.Background(),nil,auth.Identity{},uuid.Nil,uuid.Nil,dependency,sha);err==nil{t.Fatal("missing receipt dependencies accepted")}
}

func v2SignedObject(task uuid.UUID,ref v2input.Ref,media string,size int64)objectstore.Object{
	return objectstore.Object{Key:"tenants/"+uuid.New().String()+"/tasks/"+task.String()+"/attempts/1/fences/2/"+ref.ArtifactID.String(),VersionID:"version",SHA256:ref.SHA256,ContentType:media,Bytes:size}
}
