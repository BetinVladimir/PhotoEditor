package workflow

import (
 "bytes"
 "context"
 "errors"
 "sync"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type MetadataAdoptionCandidates interface {
 Next(context.Context,uuid.UUID)(*MetadataAdoptionCandidate,error)
 Committed(context.Context,MetadataAdoptionCandidate)(bool,error)
}
type MetadataAdoptionRunner interface{Run(context.Context,MetadataAdoptionCandidate)error}
type MetadataAdoptionInvalidSettler interface{SettleInvalid(context.Context,MetadataAdoptionCandidate,string)(string,error)}
var metadataTerminalCodes=map[string]bool{
 "DELIVERY_METADATA_DOCUMENT_EVIDENCE":true,"DELIVERY_METADATA_DOCUMENT_BYTES":true,
 "DELIVERY_METADATA_DOCUMENT_CANONICAL":true,"DELIVERY_METADATA_RASTER_EVIDENCE":true,
 "DELIVERY_METADATA_RASTER_STREAM":true,"DELIVERY_METADATA_RASTER_BYTES":true,
 "DELIVERY_METADATA_RESULT_ALIAS":true,"DELIVERY_METADATA_PROJECTION_BYTES":true,
 "DELIVERY_METADATA_OUTPUT_CLOSURE":true,"DELIVERY_METADATA_PHYSICAL_INVALID":true,
}
func metadataTerminalCode(code string)bool{return metadataTerminalCodes[code]}
type MetadataAdoptionDispatcher struct{Candidates MetadataAdoptionCandidates;Runner MetadataAdoptionRunner;Timeout time.Duration;mu sync.Mutex;after uuid.UUID}
func(d *MetadataAdoptionDispatcher)ProcessOne(ctx context.Context)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if d==nil{return false,v2input.ErrInvalid}
 if !d.mu.TryLock(){return false,nil};defer d.mu.Unlock()
 if d.Candidates==nil||d.Runner==nil||d.Timeout<time.Second||d.Timeout>10*time.Minute{return false,v2input.ErrInvalid}
 query,cancel:=context.WithTimeout(ctx,10*time.Second);candidate,err:=d.Candidates.Next(query,d.after);cancel()
 if err!=nil{return false,err};if candidate==nil{d.after=uuid.Nil;return false,nil}
 if !candidate.valid()||bytes.Compare(candidate.Task[:],d.after[:])<=0{return false,v2input.ErrInvalid}
 job:=*candidate;d.after=job.Task
 query,cancel=context.WithTimeout(ctx,10*time.Second);committed,err:=d.Candidates.Committed(query,job);cancel()
 if err!=nil||committed{return committed,err}
 work,stop:=context.WithTimeout(ctx,d.Timeout);runErr:=d.Runner.Run(work,job);stop()
 // Independent durable evidence decides unknown process/commit outcomes.
 receipt,finish:=context.WithTimeout(context.WithoutCancel(ctx),10*time.Second);defer finish()
 committed,err=d.Candidates.Committed(receipt,job)
 if err!=nil{return false,errors.Join(runErr,err)}
 if committed{return true,nil}
 var refusal MetadataJobRefusal
 if errors.As(runErr,&refusal)&&metadataTerminalCode(refusal.Code){
  settler,ok:=d.Candidates.(MetadataAdoptionInvalidSettler)
  if !ok{return false,v2input.ErrInvalid}
  state,err:=settler.SettleInvalid(receipt,job,refusal.Code)
  if err!=nil{return false,errors.Join(runErr,err)}
  if state=="deferred"||state=="rejected"{return false,nil}
  if state!="stale"{return false,v2input.ErrInvalid}
 }
 if runErr!=nil{return false,runErr}
 return false,v2input.ErrExpired
}
