package interchange

import (
 "bytes"
 "context"
 "sort"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/auth"
)

type parseCursor struct {entries []v2input.Entry;index int}
func(c *parseCursor)Next(ctx context.Context)(v2input.Entry,bool,error){
 if err:=ctx.Err();err!=nil{return v2input.Entry{},false,err}
 if c.index==len(c.entries){return v2input.Entry{},false,nil}
 entry:=c.entries[c.index];c.index++;return entry,true,nil
}
type ParseTransport struct {plan ParsePlan;built v2input.Built;entries []v2input.Entry}
func(p ParsePlan)PublishInputs(ctx context.Context,publisher v2input.Publisher)(ParseTransport,error){
 if err:=ctx.Err();err!=nil{return ParseTransport{},err}
 if p.tenant==uuid.Nil||p.identity.Task==uuid.Nil||publisher==nil||p.paramsRef.ArtifactID==p.params.Request.Bundle.ArtifactID{return ParseTransport{},v2input.ErrInvalid}
 entries:=p.sortedInputs()
 built,err:=v2input.BuildInputClosure(ctx,p.identity.Task,&parseCursor{entries:entries},publisher)
 if err!=nil{return ParseTransport{},err}
 if built.Count!=2||len(built.Chunks)!=1||built.Manifest.ArtifactID==p.paramsRef.ArtifactID||built.Manifest.ArtifactID==p.params.Request.Bundle.ArtifactID{return ParseTransport{},v2input.ErrInvalid}
 seen:=map[uuid.UUID]bool{p.paramsRef.ArtifactID:true,p.params.Request.Bundle.ArtifactID:true,built.Manifest.ArtifactID:true}
 for _,chunk:=range built.Chunks{if seen[chunk.ArtifactID]{return ParseTransport{},v2input.ErrInvalid};seen[chunk.ArtifactID]=true}
 return ParseTransport{plan:p,built:built,entries:entries},nil
}
func(p ParsePlan)sortedInputs()[]v2input.Entry{
 entries:=p.DirectInputs()
 sort.Slice(entries,func(i,j int)bool{
  if c:=bytes.Compare(entries[i].Ref.ArtifactID[:],entries[j].Ref.ArtifactID[:]);c!=0{return c<0}
  if entries[i].Ref.SHA256!=entries[j].Ref.SHA256{return entries[i].Ref.SHA256<entries[j].Ref.SHA256}
  return entries[i].Contract<entries[j].Contract
 })
 return entries
}
// MaterializeParseTask computes the entry digest without object IO. Task shells
// must precede scheduler transport reservations; manifest/chunk IDs are not part
// of the entry digest and will be registered independently afterwards.
func(p ParsePlan)MaterializeParseTask(ctx context.Context,tx ProofTransaction,who auth.Identity,tenant uuid.UUID,request CreateRequest,key string)error{
 digest,err:=canonical.Hash(p.sortedInputs());if err!=nil{return err}
 return (ParseTransport{plan:p,built:v2input.Built{SHA256:digest}}).MaterializeTask(ctx,tx,who,tenant,request,key)
}
func(t ParseTransport)DraftTask()(v2input.DraftTask,error){return t.plan.DraftTask(t.built.SHA256)}
func(t ParseTransport)Manifest()v2input.Ref{return t.built.Manifest}

// Uses only the owned inventory/publication receipts, never caller cursors.
// Run/task/doc shells must already exist; caller owns scheduler tenant TX and
// rolls back on error. Does not save API proof, seal graph or dispatch a task.
func(t ParseTransport)RegisterInputs(ctx context.Context,tx pgx.Tx,tenant,run uuid.UUID)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil||tenant!=t.plan.tenant||run!=t.plan.identity.Run||t.plan.identity.Task==uuid.Nil||len(t.entries)!=2{return v2input.ErrInvalid}
 built:=t.built;built.Chunks=append([]v2input.Ref(nil),built.Chunks...)
 entries:=append([]v2input.Entry(nil),t.entries...)
 open:=func(ctx context.Context)(v2input.Cursor,error){if err:=ctx.Err();err!=nil{return nil,err};return &parseCursor{entries:append([]v2input.Entry(nil),entries...)},nil}
 return v2input.RegisterInTransaction(ctx,tx,tenant,run,t.plan.identity.Task,built,open,[]v2input.Ref{t.plan.paramsRef,t.plan.params.Request.Bundle})
}
