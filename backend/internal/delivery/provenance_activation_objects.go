package delivery

import (
 "context"
 "fmt"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type SignedActivationObject struct {
 ArtifactID uuid.UUID
 Bucket string
 Object objectstore.Object
}

// ReadSignedActivationObjects obtains only the exact V31 object versions
// exposed under the current parent expansion lease and signed matrix.
func ReadSignedActivationObjects(ctx context.Context,db *pgxpool.Pool,lease ExpansionLease,
 task,params uuid.UUID,paramsSHA string,paramsRaw []byte)([]SignedActivationObject,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if db==nil||!lease.Valid()||task==uuid.Nil||params==uuid.Nil||
  !shaPattern.MatchString(paramsSHA)||len(paramsRaw)<2||len(paramsRaw)>65536||
  digest(paramsRaw)!=paramsSHA{return nil,v2input.ErrInvalid}
 rows,err:=db.Query(ctx,`SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type
  FROM photo.v30_delivery_signed_activation_objects($1,$2,$3,$4,$5,$6)`,
  lease.Expansion,lease.Token,task,params,paramsSHA,string(paramsRaw))
 if err!=nil{return nil,err}
 defer rows.Close()
 result:=make([]SignedActivationObject,0)
 for rows.Next(){
  var entry SignedActivationObject
  if err:=rows.Scan(&entry.ArtifactID,&entry.Object.SHA256,&entry.Object.Bytes,
   &entry.Bucket,&entry.Object.Key,&entry.Object.VersionID,&entry.Object.ContentType);err!=nil{return nil,err}
  if len(result)>=160000{return nil,v2input.ErrInvalid}
  result=append(result,entry)
 }
 if err:=rows.Err();err!=nil{return nil,err}
 return result,nil
}

// VerifySignedActivationInventory checks complete closure order and streams
// both retained versions per V31 receipt. A missing SQL row is never skipped.
func VerifySignedActivationInventory(ctx context.Context,tenant uuid.UUID,bucket string,
 closure SignedAssemblyClosure,inventory []SignedActivationObject,
 objects SignedDependencyObjectReader,renew func(context.Context)error)error{
 if err:=ctx.Err();err!=nil{return err}
 if tenant==uuid.Nil||closure.TenantID!=tenant||len(bucket)<3||len(bucket)>63||
  len(closure.Entries)<1||len(closure.Entries)>80000||
  len(inventory)!=2*len(closure.Entries)||objects==nil||renew==nil{return v2input.ErrInvalid}
 for index,entry:=range closure.Entries{
  if err:=ctx.Err();err!=nil{return err}
  if index%25==0{if err:=renew(ctx);err!=nil{return err}}
  signed,manifest:=inventory[2*index],inventory[2*index+1]
  if signed.ArtifactID!=entry.Signed.ArtifactID||manifest.ArtifactID!=entry.Manifest.ArtifactID||
   signed.Bucket!=bucket||manifest.Bucket!=bucket||
   signed.Object.SHA256!=entry.Signed.SHA256||manifest.Object.SHA256!=entry.Manifest.SHA256||
   entry.TaskID==uuid.Nil||entry.ResultID==uuid.Nil||
   signed.Object.Key==manifest.Object.Key{return v2input.ErrInvalid}
  receipt:=SignedDependencyReceipt{TaskID:entry.TaskID,ResultID:entry.ResultID,
   Signed:v2input.Ref{ArtifactID:entry.Signed.ArtifactID,SHA256:entry.Signed.SHA256},
   Manifest:v2input.Ref{ArtifactID:entry.Manifest.ArtifactID,SHA256:entry.Manifest.SHA256},
   ValidationSHA:entry.ValidationSHA,Bucket:bucket,
   SignedObject:signed.Object,ManifestObject:manifest.Object}
  if err:=VerifySignedDependencyObjects(ctx,tenant,receipt,objects);err!=nil{
   return fmt.Errorf("signed activation slot %d: %w",index,err)
  }
 }
 return renew(ctx)
}

// SignedActivationInventoryEqual is the second SQL read barrier after object
// IO; object versions and all metadata must still be byte-for-byte identical.
func SignedActivationInventoryEqual(before,after []SignedActivationObject)bool{
 if len(before)==0||len(before)!=len(after){return false}
 for i:=range before{if before[i]!=after[i]{return false}}
 return true
}
