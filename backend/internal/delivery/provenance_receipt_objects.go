package delivery

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io"
 "strings"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type SignedDependencyObjectReader interface{
 Open(context.Context,objectstore.Object)(io.ReadCloser,error)
}

// VerifySignedDependencyObjects reads both exact object versions to EOF.
// V31 adoption already checks the cryptographic C2PA claim; this additional
// check catches missing/replaced storage bytes before Delivery uses the refs.
func VerifySignedDependencyObjects(ctx context.Context,tenant uuid.UUID,receipt SignedDependencyReceipt,
 objects SignedDependencyObjectReader)error{
 if err:=ctx.Err();err!=nil{return err}
 if tenant==uuid.Nil||objects==nil||receipt.TaskID==uuid.Nil||receipt.ResultID==uuid.Nil||
  receipt.Signed.ArtifactID==uuid.Nil||receipt.Manifest.ArtifactID==uuid.Nil||
  receipt.Signed.ArtifactID==receipt.Manifest.ArtifactID||receipt.SignedObject.Key==receipt.ManifestObject.Key||
  (receipt.SignedObject.ContentType!="image/jpeg"&&receipt.SignedObject.ContentType!="image/png")||
  receipt.ManifestObject.ContentType!="application/c2pa"||
  receipt.SignedObject.SHA256!=receipt.Signed.SHA256||receipt.ManifestObject.SHA256!=receipt.Manifest.SHA256||
  !shaPattern.MatchString(receipt.ValidationSHA){return v2input.ErrInvalid}
 prefix:="tenants/"+tenant.String()+"/tasks/"+receipt.TaskID.String()+"/attempts/"
 for _,entry:=range []struct{object objectstore.Object;artifact uuid.UUID;maximum int64}{
  {receipt.SignedObject,receipt.Signed.ArtifactID,512<<20},
  {receipt.ManifestObject,receipt.Manifest.ArtifactID,8<<20},
 }{
  if !strings.HasPrefix(entry.object.Key,prefix)||!strings.HasSuffix(entry.object.Key,"/"+entry.artifact.String())||
   entry.object.Bytes<1||entry.object.Bytes>entry.maximum||entry.object.VersionID==""||entry.object.VersionID=="null"||
   !shaPattern.MatchString(entry.object.SHA256){return v2input.ErrInvalid}
 }
 for _,object:=range []objectstore.Object{receipt.SignedObject,receipt.ManifestObject}{
  if err:=verifySignedDependencyObject(ctx,objects,object);err!=nil{return err}
 }
 return ctx.Err()
}

func verifySignedDependencyObject(ctx context.Context,objects SignedDependencyObjectReader,object objectstore.Object)error{
 if err:=ctx.Err();err!=nil{return err}
 body,err:=objects.Open(ctx,object);if err!=nil{return err}
 hash:=sha256.New();buffer:=make([]byte,128<<10);var count int64
 for{
  if err:=ctx.Err();err!=nil{_ = body.Close();return err}
  n,readErr:=body.Read(buffer)
  if n<0||n>len(buffer){_ = body.Close();return objectstore.ErrIntegrity}
  if n>0{count+=int64(n);if count>object.Bytes{_ = body.Close();return objectstore.ErrIntegrity};_,_=hash.Write(buffer[:n])}
  if readErr!=nil{
   closeErr:=body.Close()
   if readErr!=io.EOF{return readErr}
   if closeErr!=nil{return closeErr}
   if count!=object.Bytes||hex.EncodeToString(hash.Sum(nil))!=object.SHA256{return objectstore.ErrIntegrity}
   return ctx.Err()
  }
  if n==0{_ = body.Close();return objectstore.ErrIntegrity}
 }
}
