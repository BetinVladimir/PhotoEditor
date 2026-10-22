package v2render

import (
 "context"

 "photoeditor/backend/internal/v2input"
)

// PublishAuthoredSegmentTransport is used only after a protected singleton segment/1 shell
// has committed. The supplied publisher is the existing version-verifying
// scoped backend document publisher; actual reserve/complete SQL authenticates
// that shell. This routine creates no run/task, sealed flag or ready receipt.
// Generic transport and its real chunk/manifest digest remain unchanged.
func PublishAuthoredSegmentTransport(ctx context.Context,plan AuthoredSegmentTaskPlan,publisher v2input.Publisher)(v2input.GraphTask,error){
 empty:=v2input.GraphTask{}
 if err:=ctx.Err();err!=nil{return empty,err}
 if publisher==nil{return empty,v2input.ErrInvalid}
 if err:=plan.validateFrozen();err!=nil{return empty,err}
 // Own copies across external publication and the later DB-only transaction.
 entries:=append([]v2input.Entry(nil),plan.Entries...)
 open:=func(ctx context.Context)(v2input.Cursor,error){
  if err:=ctx.Err();err!=nil{return nil,err}
  return &compositionCursor{entries:append([]v2input.Entry(nil),entries...)},nil
 }
 cursor,err:=open(ctx);if err!=nil{return empty,err}
 built,err:=v2input.BuildInputClosure(ctx,plan.Task.ID,cursor,publisher)
 if err!=nil{return empty,err}
 if err=ctx.Err();err!=nil{return empty,err}
 if err=plan.validateFrozen();err!=nil{return empty,err}
 if built.Count!=len(entries){return empty,v2input.ErrInvalid}
 built.Chunks=append([]v2input.Ref(nil),built.Chunks...)
 return v2input.GraphTask{ID:plan.Task.ID,Built:built,Open:open,Direct:[]v2input.Ref{plan.ParamsRef}},nil
}
