package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// CheckExportDraftAdmission runs within the caller's API transaction. Success
// proves current database authority only; it does not qualify native resources,
// seal the graph, mutate the task state or manufacture a creation ticket.
func CheckExportDraftAdmission(ctx context.Context,tx ProofTransaction,who auth.Identity,tenant uuid.UUID,request CreateRequest,key string,frozen FrozenExportDocuments,inventory ExportInputInventory)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil{return v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(tenant,who,request,key);if err!=nil{return err}
 _,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return err}
 if inventory.task!=compiled.task||len(inventory.entries)==0||len(inventory.entries)>v2input.MaxEntries{return v2input.ErrInvalid}
 digest,err:=canonical.Hash(inventory.entries);if err!=nil||digest!=inventory.digest{return v2input.ErrInvalid}
 entries:=map[uuid.UUID]v2input.Entry{}
 for _,entry:=range inventory.entries{
  if !validImportRef(entry.Ref)||entry.Contract==""{return v2input.ErrInvalid}
  if _,exists:=entries[entry.Ref.ArtifactID];exists{return v2input.ErrInvalid};entries[entry.Ref.ArtifactID]=entry
 }
 for _,root:=range compiled.roots{if entries[root.Ref.ArtifactID]!=root{return v2input.ErrInvalid}}
 var ready bool
 if err:=tx.QueryRow(ctx,`SELECT photo.v30_interchange_export_draft_ready($1,$2,$3,$4,$5,$6)`,tenant,who.UserID,who.SessionID,key,intent.RequestSHA,digest).Scan(&ready);err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 if !ready{return domain.NewError("INTERCHANGE_CREATE_STALE",409)}
 return nil
}
