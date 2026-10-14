package interchange
import (
 "context"
 "strings"
 "unicode"
 "unicode/utf8"
 "github.com/google/uuid"
 "golang.org/x/text/unicode/norm"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type ArchiveUploadRequest struct{
 ProjectID uuid.UUID `json:"project_id"`
 Filename string `json:"filename"`
 Bytes int64 `json:"bytes"`
 SHA256 string `json:"sha256"`
}
type ArchiveUploadIntent struct{
 tenant,actor,project,upload,artifact uuid.UUID
 key,objectKey string
 request ArchiveUploadRequest
 requestJCS []byte
 requestSHA string
}
func(p ArchiveUploadIntent)ID()uuid.UUID{return p.upload}
func(p ArchiveUploadIntent)ArtifactID()uuid.UUID{return p.artifact}
func(p ArchiveUploadIntent)ObjectKey()string{return p.objectKey}
func(p ArchiveUploadIntent)Request()ArchiveUploadRequest{return p.request}
func(p ArchiveUploadIntent)RequestBody()[]byte{return append([]byte(nil),p.requestJCS...)}
func(p ArchiveUploadIntent)RequestSHA()string{return p.requestSHA}
func(p ArchiveUploadIntent)PartsTotal()int{return int((p.request.Bytes+objectstore.PartBytes-1)/objectstore.PartBytes)}
func FreezeArchiveUpload(ctx context.Context,tid uuid.UUID,who auth.Identity,key string,request ArchiveUploadRequest)(ArchiveUploadIntent,error){
 var zero ArchiveUploadIntent
 if err:=ctx.Err();err!=nil{return zero,err}
 if tid==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||request.ProjectID==uuid.Nil||!idempotency.ValidKey(key)||request.Bytes<22||request.Bytes>objectstore.MaxInterchangeArchiveBytes||!validImportRef(v2input.Ref{ArtifactID:request.ProjectID,SHA256:request.SHA256})||!utf8.ValidString(request.Filename)||!norm.NFC.IsNormalString(request.Filename)||utf8.RuneCountInString(request.Filename)<5||utf8.RuneCountInString(request.Filename)>240||strings.ContainsAny(request.Filename,"/\\:")||!strings.HasSuffix(strings.ToLower(request.Filename),".zip"){return zero,v2input.ErrInvalid}
 for _,r:=range request.Filename{if unicode.IsControl(r){return zero,v2input.ErrInvalid}}
 raw,err:=canonical.Marshal(request);if err!=nil{return zero,err}
 upload:=uuid.NewSHA1(tid,[]byte("interchange:archive-upload:"+who.UserID.String()+":"+key))
 objectKey,err:=objectstore.InterchangeUploadKey(tid,upload);if err!=nil{return zero,err}
 return ArchiveUploadIntent{tenant:tid,actor:who.UserID,project:request.ProjectID,upload:upload,artifact:uuid.NewSHA1(upload,[]byte("archive:artifact:1")),key:key,objectKey:objectKey,request:request,requestJCS:raw,requestSHA:rawSHA(raw)},nil
}
// Freeze parts before persistence/IO. Full contiguous coverage is mandatory;
// ETags are transport receipts, never accepted as the archive SHA.
func(p ArchiveUploadIntent)CompletionParts(ctx context.Context,size int64,parts []objectstore.UploadedPart)([]objectstore.UploadedPart,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if p.upload==uuid.Nil||size!=p.request.Bytes||len(parts)!=p.PartsTotal()||len(parts)<1||len(parts)>objectstore.MaxUploadParts{return nil,v2input.ErrInvalid}
 owned:=append([]objectstore.UploadedPart(nil),parts...)
 for i,part:=range owned{if part.PartNumber!=i+1||len(part.ETag)<1||len(part.ETag)>256||strings.IndexFunc(part.ETag,func(r rune)bool{return r<0x20||r==0x7f})>=0{return nil,v2input.ErrInvalid}}
 return owned,nil
}
