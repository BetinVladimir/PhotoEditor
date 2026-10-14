package delivery

import (
 "context"
 "errors"
 "strings"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type SignedDependencyReceipt struct {
 Signed v2input.Ref
 Manifest v2input.Ref
 ValidationSHA string
 TaskID,ResultID uuid.UUID
 Bucket string
 SignedObject,ManifestObject objectstore.Object
}

func validSignedDependencyReceipt(dependency SigningDependency,receipt SignedDependencyReceipt)bool{
 return dependency.ExportID!=uuid.Nil&&dependency.Export.ArtifactID!=uuid.Nil&&dependency.PolicyID!=uuid.Nil&&
  receipt.Signed.ArtifactID!=uuid.Nil&&receipt.Manifest.ArtifactID!=uuid.Nil&&
  receipt.Signed.ArtifactID!=receipt.Manifest.ArtifactID&&
  receipt.Signed.ArtifactID!=dependency.Export.ArtifactID&&receipt.Manifest.ArtifactID!=dependency.Export.ArtifactID&&
  shaPattern.MatchString(receipt.Signed.SHA256)&&shaPattern.MatchString(receipt.Manifest.SHA256)&&
  shaPattern.MatchString(receipt.ValidationSHA)&&receipt.TaskID!=uuid.Nil&&receipt.ResultID!=uuid.Nil&&
  len(receipt.Bucket)>=3&&len(receipt.Bucket)<=63&&
  receipt.SignedObject.SHA256==receipt.Signed.SHA256&&receipt.ManifestObject.SHA256==receipt.Manifest.SHA256&&
  receipt.SignedObject.Bytes>=1&&receipt.SignedObject.Bytes<=512<<20&&
  receipt.ManifestObject.Bytes>=1&&receipt.ManifestObject.Bytes<=8<<20&&
  (receipt.SignedObject.ContentType=="image/jpeg"||receipt.SignedObject.ContentType=="image/png")&&
  receipt.ManifestObject.ContentType=="application/c2pa"&&
  receipt.SignedObject.VersionID!=""&&receipt.SignedObject.VersionID!="null"&&
  receipt.ManifestObject.VersionID!=""&&receipt.ManifestObject.VersionID!="null"&&
  strings.HasPrefix(receipt.SignedObject.Key,"tenants/")&&strings.HasPrefix(receipt.ManifestObject.Key,"tenants/")&&
  receipt.SignedObject.Key!=receipt.ManifestObject.Key
}

// ReadSignedDependencyReceipt reads only a V31 physically adopted canonical
// signer result for this exact Delivery export and policy. Object bytes and
// current operator qualification still need independent checks by the caller.
func ReadSignedDependencyReceipt(ctx context.Context,db *pgxpool.Pool,who auth.Identity,tid,project uuid.UUID,
 dependency SigningDependency,policySHA,bucket string)(SignedDependencyReceipt,bool,error){
 var receipt SignedDependencyReceipt
 if err:=ctx.Err();err!=nil{return receipt,false,err}
 if db==nil||tid==uuid.Nil||project==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||
  dependency.ExportID==uuid.Nil||dependency.Export.ArtifactID==uuid.Nil||!shaPattern.MatchString(dependency.Export.SHA256)||
  dependency.PolicyID==uuid.Nil||!shaPattern.MatchString(policySHA)||len(bucket)<3||len(bucket)>63{return receipt,false,v2input.ErrInvalid}
 found:=false
 err:=tenant.WithTenant(ctx,db,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  err:=tx.QueryRow(ctx,`SELECT signed_id,signed_sha,manifest_id,manifest_sha,validation_sha,adopted_task,adopted_result,
    bucket,signed_key,signed_version,signed_bytes,signed_media,manifest_key,manifest_version,manifest_bytes
   FROM photo.v30_delivery_signed_receipt($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
   tid,who.UserID,who.SessionID,project,dependency.ExportID,dependency.Export.ArtifactID,
   dependency.Export.SHA256,dependency.PolicyID,policySHA).Scan(
    &receipt.Signed.ArtifactID,&receipt.Signed.SHA256,&receipt.Manifest.ArtifactID,&receipt.Manifest.SHA256,
    &receipt.ValidationSHA,&receipt.TaskID,&receipt.ResultID,&receipt.Bucket,
    &receipt.SignedObject.Key,&receipt.SignedObject.VersionID,&receipt.SignedObject.Bytes,&receipt.SignedObject.ContentType,
    &receipt.ManifestObject.Key,&receipt.ManifestObject.VersionID,&receipt.ManifestObject.Bytes)
  if errors.Is(err,pgx.ErrNoRows){return nil}
  if err!=nil{return err}
  found=true
  receipt.SignedObject.SHA256=receipt.Signed.SHA256
  receipt.ManifestObject.SHA256=receipt.Manifest.SHA256
  if receipt.Bucket!=bucket||!validSignedDependencyReceipt(dependency,receipt){return v2input.ErrInvalid}
  return nil
 })
 if err!=nil{return SignedDependencyReceipt{},false,err}
 if !found{return SignedDependencyReceipt{},false,nil}
 return receipt,true,ctx.Err()
}
