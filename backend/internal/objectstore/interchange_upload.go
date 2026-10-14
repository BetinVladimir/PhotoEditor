package objectstore
import (
 "context"
 "net"
 "net/url"
 "strings"
 "time"
 "unicode"
 "unicode/utf8"
 "github.com/google/uuid"
)
// ValidInterchangeMultipartID is shared by V30 receipt, recovery and drain paths.
func ValidInterchangeMultipartID(id string)bool{
 if id==""||len(id)>1024||!utf8.ValidString(id){return false}
 for _,r:=range id{if unicode.IsControl(r){return false}};return true
}
const MaxInterchangeArchiveBytes=int64(64<<30)
func InterchangeUploadKey(tenant,upload uuid.UUID)(string,error){
 if tenant==uuid.Nil||upload==uuid.Nil{return "",ErrInvalid}
 return "tenants/"+tenant.String()+"/interchange-uploads/"+upload.String()+"/archive.zip",nil
}
func validInterchangeUploadKey(key string)bool{
 parts:=strings.Split(key,"/");if len(parts)!=5||parts[0]!="tenants"||parts[2]!="interchange-uploads"||parts[4]!="archive.zip"{return false}
 tenant,err:=uuid.Parse(parts[1]);if err!=nil{return false};upload,err:=uuid.Parse(parts[3]);if err!=nil{return false}
 expected,err:=InterchangeUploadKey(tenant,upload);return err==nil&&key==expected
}
func(s *Store)CompleteInterchangeMultipart(ctx context.Context,key,uploadID string,size int64,parts []UploadedPart)(PendingObject,error){
 if !validInterchangeArchiveKey(key)||size<22{return PendingObject{},ErrInvalid}
 return s.completeMultipartBounded(ctx,key,uploadID,"application/zip",size,parts,MaxInterchangeArchiveBytes)
}
func(s *Store)RecoverInterchangeMultipart(ctx context.Context,key string,size int64)(PendingObject,error){
 if !validInterchangeArchiveKey(key)||size<22{return PendingObject{},ErrInvalid}
 return s.recoverCompletedMultipartBounded(ctx,key,"application/zip",size,MaxInterchangeArchiveBytes)
}
func(s *Store)VerifyInterchangePending(ctx context.Context,pending PendingObject,expectedSHA string)(Object,error){
 if !validInterchangeArchiveKey(pending.Key)||pending.Bytes<22||pending.ContentType!="application/zip"||!hashPattern.MatchString(expectedSHA){return Object{},ErrInvalid}
 return s.verifyPendingBounded(ctx,pending,expectedSHA,MaxInterchangeArchiveBytes)
}

// BeginInterchangeMultipart accepts identity, not an arbitrary object key.
// Callers must reserve the upload ledger before IO and bind the returned ID.
func(s *Store)BeginInterchangeMultipart(ctx context.Context,tenant,upload uuid.UUID)(string,error){
 if err:=ctx.Err();err!=nil{return "",err}
 ctx=singleInterchangeCreateContext(ctx)
 key,err:=InterchangeUploadKey(tenant,upload);if err!=nil{return "",err}
 id,err:=s.BeginMultipart(ctx,key,"application/zip");if err!=nil{return "",err};if !ValidInterchangeMultipartID(id){return "",ErrIntegrity};return id,nil
}

// SignInterchangeParts must follow a durable signing authorization. Its absolute
// notAfter must be no later than the upload expiry and recorded before signing.
func(s *Store)SignInterchangeParts(ctx context.Context,tenant,upload uuid.UUID,multipart string,size int64,parts []int,notAfter time.Time)([]PartURL,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 key,err:=InterchangeUploadKey(tenant,upload);if err!=nil{return nil,err}
 now:=time.Now().UTC();ttl,err:=interchangePartsTTL(now,notAfter,size,parts);if err!=nil{return nil,err}
 // Do not issue URLs after the authorized horizon. The 1s margin covers the
 // ordinary signing interval; the returned expiry is independently checked.
 signing,stop:=context.WithDeadline(ctx,notAfter);defer stop()
 urls,err:=s.SignUploadParts(signing,key,multipart,parts,ttl);if err!=nil{return nil,err}
 if err:=signing.Err();err!=nil{return nil,err}
 for _,url:=range urls{if url.ExpiresAt.After(notAfter){return nil,ErrIntegrity}}
 return urls,nil
}
func interchangePartsTTL(now,notAfter time.Time,size int64,parts []int)(time.Duration,error){
 if size<22||size>MaxInterchangeArchiveBytes||len(parts)<1||len(parts)>100||notAfter.IsZero(){return 0,ErrInvalid}
 total:=(size+PartBytes-1)/PartBytes;seen:=make(map[int]bool,len(parts))
 for _,part:=range parts{if part<1||int64(part)>total||seen[part]{return 0,ErrInvalid};seen[part]=true}
 ttl:=notAfter.Sub(now)-time.Second;if ttl>15*time.Minute{ttl=15*time.Minute}
 if ttl<time.Minute{return 0,ErrInvalid}
 return ttl,nil
}

// AbortInterchangeMultipart targets exactly one losing session. It must never
// be replaced by RemoveIncompleteUpload, which aborts all sessions for a key.
func(s *Store)AbortInterchangeMultipart(ctx context.Context,tenant,upload uuid.UUID,multipart string)error{
 key,err:=InterchangeUploadKey(tenant,upload);if err!=nil{return err}
 return s.AbortMultipart(ctx,key,multipart)
}

// ValidInterchangePartURL validates the public signed-link envelope, not the
// signature or storage authority. Callers must still bind exact requested parts.
func ValidInterchangePartURL(part PartURL,now,notAfter time.Time)bool{
 if part.PartNumber<1||part.PartNumber>4096||len(part.URL)==0||len(part.URL)>32768||now.IsZero()||!part.ExpiresAt.After(now.Add(5*time.Second))||part.ExpiresAt.After(now.Add(15*time.Minute))||(!notAfter.IsZero()&&part.ExpiresAt.After(notAfter)){return false}
 parsed,err:=url.Parse(part.URL)
 if err!=nil||parsed.Hostname()==""||parsed.User!=nil||parsed.Fragment!=""||parsed.Opaque!=""||strings.ContainsAny(part.URL,"\r\n\x00"){return false}
 if parsed.Scheme=="https"{return true}
 if parsed.Scheme!="http"{return false}
 host:=parsed.Hostname();ip:=net.ParseIP(host)
 return strings.EqualFold(host,"localhost")||(ip!=nil&&ip.IsLoopback())
}
