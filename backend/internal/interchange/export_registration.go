package interchange

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// RegisterInputs registers exact transport receipts in a scheduler transaction.
// Caller must roll back on any failure. This seals task inputs, not the run graph;
// export qualification/authority must still be checked by graph admission.
func(t ExportTransport)RegisterInputs(ctx context.Context,tx pgx.Tx,tenant uuid.UUID,intent CreateIntent,frozen FrozenExportDocuments)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil||tenant==uuid.Nil||t.task!=intent.Identity.Task||t.inventory.task!=t.task{return v2input.ErrInvalid}
 _,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return err}
 if compiled.task!=t.task{return v2input.ErrInvalid}
 entries:=t.Entries();digest,err:=canonical.Hash(entries)
 if err!=nil||digest!=t.inventory.digest||digest!=t.built.SHA256||len(entries)!=t.built.Count{return v2input.ErrInvalid}
 seen:=map[uuid.UUID]v2input.Entry{}
 for _,entry:=range entries{if _,exists:=seen[entry.Ref.ArtifactID];exists{return v2input.ErrInvalid};seen[entry.Ref.ArtifactID]=entry}
 for _,root:=range compiled.roots{if seen[root.Ref.ArtifactID]!=root{return v2input.ErrInvalid}}
 var payload exportFreezePayload
 if err:=json.Unmarshal(frozen.Body(),&payload);err!=nil{return err}
 roots:=[]v2input.Ref{compiled.paramsRef,compiled.planRef,payload.Snapshot}
 open:=func(ctx context.Context)(v2input.Cursor,error){
  if err:=ctx.Err();err!=nil{return nil,err}
  return &parseCursor{entries:append([]v2input.Entry(nil),entries...)},nil
 }
 return v2input.RegisterInTransaction(ctx,tx,tenant,intent.Identity.Run,t.task,t.Built(),open,roots)
}
