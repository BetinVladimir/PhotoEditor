package provenance

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io"
 "os"
 "path/filepath"
 "sync"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)
type SigningObjects interface{Open(context.Context,objectstore.Object)(io.ReadCloser,error)}
type SigningScratch struct{root string;max,used int64;mu sync.Mutex}
func NewSigningScratch(root string,max int64)(*SigningScratch,error){
 if !filepath.IsAbs(root)||max<1||max>128<<30{return nil,ErrInvalid}
 info,err:=os.Lstat(root);if err!=nil{return nil,err};if !info.IsDir(){return nil,ErrInvalid}
 return &SigningScratch{root:root,max:max},nil
}
type StagedSigningExport struct{path,dir string;release func();mu sync.Mutex;closed bool}
func(s *StagedSigningExport)Path()string{if s==nil{return ""};return s.path}
func(s *StagedSigningExport)Close()error{
 if s==nil{return nil};s.mu.Lock();defer s.mu.Unlock()
 if s.closed{return nil}
 if err:=os.RemoveAll(s.dir);err!=nil{return err}
 s.release();s.closed=true;return nil
}
type signingContextReader struct{ctx context.Context;reader io.Reader}
func(r signingContextReader)Read(p []byte)(int,error){if err:=r.ctx.Err();err!=nil{return 0,err};return r.reader.Read(p)}
// Stage requires authority on both sides of physical IO. No user-controlled
// filename reaches the filesystem. The returned path is private and must be
// closed on all process/verification/publication outcomes.
func(s *SigningScratch)Stage(ctx context.Context,who auth.Identity,ready ReadySigningExport,catalog SigningExportCatalog,objects SigningObjects)(*StagedSigningExport,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s==nil||catalog==nil||objects==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||!ref(ready.Ref)||ready.Object.SHA256!=ready.Ref.SHA256||ready.Object.Key==""||ready.Object.VersionID==""||ready.Object.VersionID=="null"||ready.Object.Bytes<1||ready.Object.Bytes>512<<20||ready.Object.ContentType!="image/jpeg"&&ready.Object.ContentType!="image/png"{return nil,ErrInvalid}
 current,err:=catalog.LookupSigningExport(ctx,who,ready.Tenant,ready.ExportID);if err!=nil{return nil,err};if current!=ready{return nil,ErrInvalid}
 s.mu.Lock();if ready.Object.Bytes>s.max-s.used{s.mu.Unlock();return nil,ErrInvalid};s.used+=ready.Object.Bytes;s.mu.Unlock()
 release:=func(){s.mu.Lock();s.used-=ready.Object.Bytes;s.mu.Unlock()}
 dir,err:=os.MkdirTemp(s.root,"sign-export-");if err!=nil{release();return nil,err}
 staged:=&StagedSigningExport{dir:dir,path:filepath.Join(dir,"input"),release:release}
 success:=false;defer func(){if !success{_ = staged.Close()}}()
 file,err:=os.OpenFile(staged.path,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if err!=nil{return nil,err}
 stream,err:=objects.Open(ctx,ready.Object)
 if err!=nil{if stream!=nil{_ = stream.Close()};_ = file.Close();return nil,err}
 if stream==nil{_ = file.Close();return nil,ErrInvalid}
 hash:=sha256.New()
 size,copyErr:=io.Copy(io.MultiWriter(file,hash),io.LimitReader(signingContextReader{ctx:ctx,reader:stream},ready.Object.Bytes))
 var extra [1]byte;extraN,extraErr:=signingContextReader{ctx:ctx,reader:stream}.Read(extra[:])
 closeErr:=stream.Close();syncErr:=file.Sync();fileErr:=file.Close()
 if err:=ctx.Err();err!=nil{return nil,err}
 if copyErr!=nil{return nil,copyErr};if closeErr!=nil{return nil,closeErr};if syncErr!=nil{return nil,syncErr};if fileErr!=nil{return nil,fileErr}
 if extraN!=0||extraErr!=io.EOF||size!=ready.Object.Bytes||hex.EncodeToString(hash.Sum(nil))!=ready.Ref.SHA256{return nil,ErrInvalid}
 current,err=catalog.LookupSigningExport(ctx,who,ready.Tenant,ready.ExportID);if err!=nil{return nil,err};if current!=ready{return nil,ErrInvalid}
 if err:=ctx.Err();err!=nil{return nil,err}
 success=true;return staged,nil
}
