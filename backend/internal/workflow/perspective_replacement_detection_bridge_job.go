package workflow

import (
 "context"
 "errors"
 "net"
 "os"
 "path/filepath"
 "time"
 "photoeditor/backend/internal/v2input"
)

type ReplacementDetectionBridgeJob struct{Root string;Timeout time.Duration}
// Run creates an ephemeral private Unix endpoint for one operator-configured
// replay job. The runner receives the job capability directly, never via argv
// or logs. It must finish its Python process before returning. No public listen.
func(j ReplacementDetectionBridgeJob) Run(ctx context.Context,source ReplacementDetectionDocuments,runner func(context.Context,string,string)error)error{
 if err:=ctx.Err();err!=nil{return err}
 if runner==nil||!filepath.IsAbs(j.Root)||j.Timeout<=0||j.Timeout>10*time.Minute{return v2input.ErrInvalid}
 root,err:=filepath.EvalSymlinks(j.Root);if err!=nil{return err}
 if root!=filepath.Clean(j.Root){return v2input.ErrInvalid}
 info,err:=os.Stat(root);if err!=nil{return err};if !info.IsDir()||info.Mode().Perm()&0022!=0{return v2input.ErrInvalid}
 bridge,token,err:=NewReplacementDetectionAdoptionBridge(ctx,source);if err!=nil{return err}
 directory,err:=os.MkdirTemp(root,"adopt-");if err!=nil{return err}
 defer os.RemoveAll(directory)
 if err=os.Chmod(directory,0700);err!=nil{return err}
 path:=filepath.Join(directory,"rpc.sock")
 if len([]byte(path))>100{return v2input.ErrInvalid}
 listener,err:=net.ListenUnix("unix",&net.UnixAddr{Name:path,Net:"unix"});if err!=nil{return err}
 defer listener.Close()
 if err=os.Chmod(path,0600);err!=nil{return err}
 bounded,cancel:=context.WithTimeout(ctx,j.Timeout);defer cancel()
 finished:=make(chan error,1)
 go func(){err:=bridge.Serve(bounded,listener);finished<-err;cancel()}()
 runErr:=runner(bounded,path,token)
 cancel();_ = listener.Close()
 serveErr:=<-finished
 if runErr!=nil{return runErr}
 if ctx.Err()!=nil{return ctx.Err()}
 if serveErr!=nil&&!errors.Is(serveErr,context.Canceled)&&!errors.Is(serveErr,net.ErrClosed){return serveErr}
 return nil
}
