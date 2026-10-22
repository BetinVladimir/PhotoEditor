package interchange
import (
 "bytes"
 "context"
 "io"
 "os/exec"
 "path/filepath"
 "time"
 "photoeditor/backend/internal/v2input"
)
type ExportNativeProbe interface{RunExportNativeProbe(context.Context,[]byte)([]byte,error)}
type PythonExportNativeProbe struct{Python,WorkerRoot string;Timeout time.Duration;ICCRoot string}
type PythonExportMaskProbe struct{Python,WorkerRoot string;Timeout time.Duration}
type PythonExportArrayProbe struct{Python,WorkerRoot string;Timeout time.Duration}
type PythonExportStyleProbe struct{Python,WorkerRoot string;Timeout time.Duration;ICCRoot string}
type exportProbeOutput struct{body []byte;cancel context.CancelFunc}
func(w *exportProbeOutput)Write(p []byte)(int,error){
 if len(p)>4096-len(w.body){w.cancel();return 0,io.ErrShortBuffer};w.body=append(w.body,p...);return len(p),nil
}
// No shell or inherited credentials. Deployment installs the qualified Python
// environment and native libraries at these explicit absolute paths.
func(p PythonExportNativeProbe)RunExportNativeProbe(ctx context.Context,request []byte)([]byte,error){
 return p.runModule(ctx,request,"photo_worker.operations.interchange.export_image_probe")
}
func(p PythonExportMaskProbe)RunExportNativeProbe(ctx context.Context,request []byte)([]byte,error){
 return (PythonExportNativeProbe{Python:p.Python,WorkerRoot:p.WorkerRoot,Timeout:p.Timeout}).runModule(ctx,request,"photo_worker.operations.interchange.export_mask_probe")
}
func(p PythonExportArrayProbe)RunExportNativeProbe(ctx context.Context,request []byte)([]byte,error){
 return (PythonExportNativeProbe{Python:p.Python,WorkerRoot:p.WorkerRoot,Timeout:p.Timeout}).runModule(ctx,request,"photo_worker.operations.interchange.export_array_probe")
}
func(p PythonExportStyleProbe)RunExportNativeProbe(ctx context.Context,request []byte)([]byte,error){
 return (PythonExportNativeProbe{Python:p.Python,WorkerRoot:p.WorkerRoot,Timeout:p.Timeout,ICCRoot:p.ICCRoot}).runModule(ctx,request,"photo_worker.operations.interchange.export_style_probe")
}
func(p PythonExportNativeProbe)runModule(ctx context.Context,request []byte,module string)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if !filepath.IsAbs(p.Python)||!filepath.IsAbs(p.WorkerRoot)||p.Timeout<=0||p.Timeout>10*time.Minute||len(request)<2||len(request)>8192{return nil,v2input.ErrInvalid}
 bounded,cancel:=context.WithTimeout(ctx,p.Timeout);defer cancel()
 output:=&exportProbeOutput{cancel:cancel}
 command:=exec.CommandContext(bounded,p.Python,"-m",module)
 command.Dir=p.WorkerRoot
 command.Env=[]string{"PATH=/usr/bin:/bin","PYTHONPATH="+p.WorkerRoot,"PYTHONDONTWRITEBYTECODE=1","PYTHONNOUSERSITE=1","PYTHONHASHSEED=0","VIPS_CONCURRENCY=1"}
 if p.ICCRoot!="" {
  if !filepath.IsAbs(p.ICCRoot){return nil,v2input.ErrInvalid}
  command.Env=append(command.Env,"PHOTO_ICC_ROOT="+p.ICCRoot)
 }
 command.Stdin=bytes.NewReader(append([]byte(nil),request...));command.Stdout=output;command.Stderr=io.Discard;command.WaitDelay=2*time.Second
 if err:=command.Run();err!=nil{if bounded.Err()!=nil{return nil,bounded.Err()};return nil,err}
 if err:=bounded.Err();err!=nil{return nil,err};return append([]byte(nil),output.body...),nil
}
