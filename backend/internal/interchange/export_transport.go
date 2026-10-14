package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Publication is outside the task activation transaction. The publisher must
// reserve deterministic task-scoped documents and pin their physical bytes.
type ExportTransport struct{task uuid.UUID;built v2input.Built;inventory ExportInputInventory}
func(i ExportInputInventory)InputDigest()string{return i.digest}
func(i ExportInputInventory)PublishInputs(ctx context.Context,publisher v2input.Publisher)(ExportTransport,error){
 var zero ExportTransport
 if err:=ctx.Err();err!=nil{return zero,err}
 if i.task==uuid.Nil||publisher==nil||len(i.entries)==0||len(i.entries)>v2input.MaxEntries{return zero,v2input.ErrInvalid}
 owned:=append([]v2input.Entry(nil),i.entries...)
 digest,err:=canonical.Hash(owned);if err!=nil||digest!=i.digest{return zero,v2input.ErrInvalid}
 built,err:=v2input.BuildInputClosure(ctx,i.task,&parseCursor{entries:owned},publisher)
 if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 if built.Count!=len(owned)||built.SHA256!=digest{return zero,v2input.ErrInvalid}
 seen:=map[uuid.UUID]bool{}
 for _,entry:=range owned{seen[entry.Ref.ArtifactID]=true}
 for _,ref:=range append([]v2input.Ref{built.Manifest},built.Chunks...){
  if !validImportRef(ref)||seen[ref.ArtifactID]{return zero,v2input.ErrInvalid}
  seen[ref.ArtifactID]=true
 }
 built.Chunks=append([]v2input.Ref(nil),built.Chunks...)
 return ExportTransport{task:i.task,built:built,inventory:ExportInputInventory{task:i.task,entries:owned,digest:digest}},nil
}
func(t ExportTransport)TaskID()uuid.UUID{return t.task}
func(t ExportTransport)Built()v2input.Built{built:=t.built;built.Chunks=append([]v2input.Ref(nil),built.Chunks...);return built}
func(t ExportTransport)Entries()[]v2input.Entry{return t.inventory.Entries()}
