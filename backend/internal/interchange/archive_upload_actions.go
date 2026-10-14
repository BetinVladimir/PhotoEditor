package interchange
import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type ArchiveUploadActionsRepository interface{
 Lookup(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID)(ArchiveUploadRecord,error)
 Queue(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,[]objectstore.UploadedPart)error
 Abort(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent)error
}
type ArchivePartsAuthority interface{
 Authorize(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID,[]int)(ArchivePartsPermit,error)
}
type ArchiveBeginGenerationReader interface{
 Read(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID)(*ArchiveBeginGenerationSnapshot,error)
}
type ArchiveBeginGenerationStatus struct{State,MultipartID string}
type ArchiveBeginGenerationStatusReader interface{
 ReadStatus(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID)(*ArchiveBeginGenerationStatus,error)
}
type ArchiveOpenBeginStatusReader interface{
 Open(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID)(bool,error)
}
type ArchiveUploadActions struct{Repository ArchiveUploadActionsRepository;Authority ArchivePartsAuthority;Signer ArchivePartsSigner;BeginGenerations ArchiveBeginGenerationReader;GenerationStatus ArchiveBeginGenerationStatusReader;OpenBegin ArchiveOpenBeginStatusReader}
func(s ArchiveUploadActions)load(ctx context.Context,who auth.Identity,tid,pid,upload uuid.UUID,requestSHA string)(ArchiveUploadRecord,error){
 var zero ArchiveUploadRecord
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Repository==nil||tid==uuid.Nil||pid==uuid.Nil||upload==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return zero,v2input.ErrInvalid}
 record,err:=s.Repository.Lookup(ctx,who,tid,pid,upload);if err!=nil{return zero,err}
 p,err:=FreezeArchiveUpload(ctx,tid,who,record.Intent.key,record.Intent.Request());if err!=nil{return zero,err}
 if p.project!=pid||p.ID()!=upload||p.ID()!=record.Intent.ID()||p.RequestSHA()!=record.Intent.RequestSHA()||p.ObjectKey()!=record.Intent.ObjectKey()||record.Reservation.Token==uuid.Nil{return zero,objectstore.ErrIntegrity}
 if _,err:=archiveStorageGeneration(tid,upload,record.StorageKey);err!=nil{return zero,objectstore.ErrIntegrity}
 if requestSHA!=p.RequestSHA(){return zero,artifactjson.ErrConflict}
 record.Intent=p;return record,ctx.Err()
}
func(s ArchiveUploadActions)Parts(ctx context.Context,who auth.Identity,tid,pid,upload uuid.UUID,requestSHA string,parts []int)([]objectstore.PartURL,error){
 if s.Authority==nil||s.Signer==nil{return nil,v2input.ErrInvalid}
 record,err:=s.load(ctx,who,tid,pid,upload,requestSHA);if err!=nil{return nil,err}
 if record.Reservation.State!="uploading"{return nil,artifactjson.ErrStale}
 numbers,err:=archivePartNumbers(record.Intent,parts);if err!=nil{return nil,err}
 owned:=make([]int,len(numbers));for i,n:=range numbers{owned[i]=int(n)}
 candidate:=append([]int(nil),owned...)
 permit,err:=s.Authority.Authorize(ctx,who,tid,record.Intent,record.Reservation.Token,candidate);if err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err}
 for i,n:=range owned{if candidate[i]!=n{return nil,objectstore.ErrIntegrity}}
 if permit.tenant!=tid||permit.upload!=upload||permit.key!=record.StorageKey||permit.multipart!=record.Reservation.MultipartID||permit.size!=record.Intent.Request().Bytes||len(permit.parts)!=len(owned){return nil,objectstore.ErrIntegrity}
 for i,n:=range owned{if permit.parts[i]!=n{return nil,objectstore.ErrIntegrity}}
 urls,err:=permit.Sign(ctx,s.Signer);if err!=nil{return nil,err};if err:=ctx.Err();err!=nil{return nil,err}
 if len(urls)!=len(owned){return nil,objectstore.ErrIntegrity}
 expected:=make(map[int]bool,len(owned));for _,n:=range owned{expected[n]=true}
 now:=time.Now().UTC()
 for _,url:=range urls{if !expected[url.PartNumber]||!objectstore.ValidInterchangePartURL(url,now,permit.notAfter){return nil,objectstore.ErrIntegrity};delete(expected,url.PartNumber)}
 fresh,err:=s.load(ctx,who,tid,pid,upload,requestSHA);if err!=nil{return nil,err}
 if fresh.Reservation.State!="uploading"||fresh.Reservation.Token!=record.Reservation.Token||fresh.Reservation.MultipartID!=record.Reservation.MultipartID||fresh.StorageKey!=record.StorageKey{return nil,artifactjson.ErrStale}
 if err:=ctx.Err();err!=nil{return nil,err}
 return append([]objectstore.PartURL(nil),urls...),nil
}
func(s ArchiveUploadActions)Complete(ctx context.Context,who auth.Identity,tid,pid,upload uuid.UUID,requestSHA string,parts []objectstore.UploadedPart)error{
 record,err:=s.load(ctx,who,tid,pid,upload,requestSHA);if err!=nil{return err}
 switch record.Reservation.State{case "uploading","completing","verified":default:return artifactjson.ErrStale}
 owned,err:=record.Intent.CompletionParts(ctx,record.Intent.Request().Bytes,parts);if err!=nil{return err}
 if err:=s.Repository.Queue(ctx,who,tid,record.Intent,owned);err!=nil{return err}
 return ctx.Err()
}

func(s ArchiveUploadActions)Abort(ctx context.Context,who auth.Identity,tid,pid,upload uuid.UUID,requestSHA string)(bool,error){
 record,err:=s.load(ctx,who,tid,pid,upload,requestSHA);if err!=nil{return false,err}
 switch record.Reservation.State{case "reserved","uploading","completing","aborting","aborted":default:return false,artifactjson.ErrStale}
 if err:=s.Repository.Abort(ctx,who,tid,record.Intent);err!=nil{return false,err}
 // Read committed status after cancellation; cleanup may have won a replay race.
 fresh,err:=s.load(ctx,who,tid,pid,upload,requestSHA);if err!=nil{return false,err}
 switch fresh.Reservation.State{case "aborting":return true,ctx.Err();case "aborted":return false,ctx.Err();default:return false,objectstore.ErrIntegrity}
}
