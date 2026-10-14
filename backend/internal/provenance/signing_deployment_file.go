package provenance

import (
 "context"
 "io"
 "os"
 "path/filepath"
 "syscall"
)

func readPinnedSigningDeployment(ctx context.Context,path,expectedSHA string)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if !filepath.IsAbs(path)||filepath.Clean(path)!=path||!digest(expectedSHA){return nil,ErrInvalid}
 acceptable:=func(info os.FileInfo)bool{
  if !info.Mode().IsRegular()||info.Mode().Perm()&0022!=0||info.Size()<2||info.Size()>8<<20{return false}
  identity,ok:=info.Sys().(*syscall.Stat_t);return ok&&identity.Nlink==1
 }
 initial,err:=os.Lstat(path);if err!=nil{return nil,err};if !acceptable(initial){return nil,ErrInvalid}
 file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK,0);if err!=nil{return nil,err}
 defer file.Close()
 opened,err:=file.Stat();if err!=nil{return nil,err}
 if !acceptable(opened)||!os.SameFile(initial,opened){return nil,ErrInvalid}
 raw,err:=io.ReadAll(io.LimitReader(file,(8<<20)+1));if err!=nil{return nil,err}
 if err=ctx.Err();err!=nil{return nil,err}
 final,err:=file.Stat();if err!=nil{return nil,err}
 current,err:=os.Lstat(path);if err!=nil{return nil,err}
 if !acceptable(final)||!acceptable(current)||!os.SameFile(opened,final)||!os.SameFile(opened,current)||opened.Size()!=final.Size()||opened.Size()!=current.Size()||int64(len(raw))!=opened.Size()||!opened.ModTime().Equal(final.ModTime())||!opened.ModTime().Equal(current.ModTime())||bodyDigest(raw)!=expectedSHA{return nil,ErrInvalid}
 if err=ctx.Err();err!=nil{return nil,err}
 return raw,nil
}
func LoadSigningAPIDeploymentDocument(ctx context.Context,path,expectedSHA string)(SigningAPIDeploymentDocument,error){
 raw,err:=readPinnedSigningDeployment(ctx,path,expectedSHA);if err!=nil{return SigningAPIDeploymentDocument{},err}
 return DecodeSigningAPIDeploymentDocument(ctx,raw,expectedSHA)
}
