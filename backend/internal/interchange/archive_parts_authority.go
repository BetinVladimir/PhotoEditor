package interchange

import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// A permit is returned only after tenant.WithTenant commits. Fields are private
// so callers cannot manufacture an authorization for another physical upload.
type ArchivePartsPermit struct{
 tenant,upload uuid.UUID
 bucket,key,multipart string
 size int64
 parts []int
 notAfter time.Time
}
type SQLArchivePartsAuthority struct{DB *pgxpool.Pool;Bucket string}
func archivePartNumbers(intent ArchiveUploadIntent,parts []int)([]int32,error){
 if intent.ID()==uuid.Nil||len(parts)<1||len(parts)>100{return nil,v2input.ErrInvalid}
 owned:=make([]int32,len(parts));seen:=make(map[int]bool,len(parts))
 for i,n:=range parts{if n<1||n>intent.PartsTotal()||seen[n]{return nil,v2input.ErrInvalid};seen[n]=true;owned[i]=int32(n)}
 return owned,nil
}
func(s SQLArchivePartsAuthority)Authorize(ctx context.Context,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent,token uuid.UUID,parts []int)(ArchivePartsPermit,error){
 var zero ArchivePartsPermit
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63||token==uuid.Nil{return zero,v2input.ErrInvalid}
 frozen,err:=FreezeArchiveUpload(ctx,tid,who,intent.key,intent.Request());if err!=nil{return zero,err}
 if frozen.ID()!=intent.ID()||frozen.RequestSHA()!=intent.RequestSHA()||frozen.ObjectKey()!=intent.ObjectKey(){return zero,v2input.ErrInvalid}
 numbers,err:=archivePartNumbers(frozen,parts);if err!=nil{return zero,err}
 permit:=ArchivePartsPermit{tenant:tid,upload:frozen.ID(),parts:make([]int,len(numbers))}
 for i,n:=range numbers{permit.parts[i]=int(n)}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  err:=tx.QueryRow(ctx,`SELECT bucket,object_key,multipart_id,expected_bytes,not_after FROM photo.authorize_v30_archive_parts($1,$2,$3,$4,$5,$6,$7)`,tid,who.UserID,who.SessionID,frozen.ID(),token,frozen.RequestSHA(),numbers).Scan(&permit.bucket,&permit.key,&permit.multipart,&permit.size,&permit.notAfter)
  if err==pgx.ErrNoRows{return artifactjson.ErrStale};if err!=nil{return err}
  if permit.bucket!=s.Bucket||permit.size!=frozen.Request().Bytes||!objectstore.ValidInterchangeMultipartID(permit.multipart)||permit.notAfter.IsZero(){return objectstore.ErrIntegrity}
  if _,err:=archiveStorageGeneration(tid,frozen.ID(),permit.key);err!=nil{return objectstore.ErrIntegrity}
  return nil
 })
 if err!=nil{return zero,err};if err:=ctx.Err();err!=nil{return zero,err}
 return permit,nil
}

type ArchivePartsSigner interface{SignInterchangeParts(context.Context,uuid.UUID,uuid.UUID,string,int64,[]int,time.Time)([]objectstore.PartURL,error)}
type ArchiveGenerationPartsSigner interface{SignInterchangeGenerationParts(context.Context,uuid.UUID,uuid.UUID,uuid.UUID,string,int64,[]int,time.Time)([]objectstore.PartURL,error)}
func(p ArchivePartsPermit)Sign(ctx context.Context,signer ArchivePartsSigner)([]objectstore.PartURL,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 generation,err:=archiveStorageGeneration(p.tenant,p.upload,p.key)
 if err!=nil||signer==nil||len(p.bucket)<3||len(p.bucket)>63||!objectstore.ValidInterchangeMultipartID(p.multipart)||p.size<22||p.size>objectstore.MaxInterchangeArchiveBytes||len(p.parts)<1||len(p.parts)>100||!p.notAfter.After(time.Now().UTC().Add(time.Minute)){return nil,v2input.ErrInvalid}
 expected:=make(map[int]bool,len(p.parts))
 total:=(p.size+objectstore.PartBytes-1)/objectstore.PartBytes
 for _,part:=range p.parts{if part<1||int64(part)>total||expected[part]{return nil,v2input.ErrInvalid};expected[part]=true}
 candidate:=append([]int(nil),p.parts...)
 var urls []objectstore.PartURL
 if generation==uuid.Nil{urls,err=signer.SignInterchangeParts(ctx,p.tenant,p.upload,p.multipart,p.size,candidate,p.notAfter)}else{
  scoped,ok:=signer.(ArchiveGenerationPartsSigner);if !ok{return nil,v2input.ErrInvalid}
  urls,err=scoped.SignInterchangeGenerationParts(ctx,p.tenant,p.upload,generation,p.multipart,p.size,candidate,p.notAfter)
 }
 if err!=nil{return nil,err};if err:=ctx.Err();err!=nil{return nil,err}
 for i,part:=range p.parts{if candidate[i]!=part{return nil,objectstore.ErrIntegrity}}
 if len(urls)!=len(expected){return nil,objectstore.ErrIntegrity}
 now:=time.Now().UTC()
 for _,url:=range urls{if !expected[url.PartNumber]||!objectstore.ValidInterchangePartURL(url,now,p.notAfter){return nil,objectstore.ErrIntegrity};delete(expected,url.PartNumber)}
 return append([]objectstore.PartURL(nil),urls...),nil
}
