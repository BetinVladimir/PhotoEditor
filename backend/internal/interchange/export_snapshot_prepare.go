package interchange

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// Authority must check active tenant/project/download permission, source lineage,
// and every pinned recipe's typed object closure under the current principal.
// It must never substitute a newer source or recipe for the submitted pins.
type ExportSnapshotAuthority interface {
 CheckExportSnapshot(context.Context,auth.Identity,uuid.UUID,uuid.UUID,CompiledExportSnapshot)error
}
// Publication reserves deterministic identity/content, writes immutable bytes,
// recovers uncertain writes, verifies version/SHA/size and returns the root ref.
// It does not make the snapshot available for export by itself.
type ExportSnapshotPublication interface {
 PublishExportSnapshot(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,CompiledExportSnapshot)(v2input.Ref,error)
}
// Attach MUST recheck authority and exact snapshot item pins in its transaction,
// reject a changed canonical request for the same key, and atomically bind the
// verified object root with snapshot_items. No object IO under database locks.
type ExportSnapshotAttachment interface {
 AttachExportSnapshot(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,CompiledExportSnapshot,v2input.Ref)error
}
type ExportSnapshotPreparation struct {
 Authority ExportSnapshotAuthority
 Publication ExportSnapshotPublication
 Attachment ExportSnapshotAttachment
}
type ExportSnapshotReceipt struct {
 ID uuid.UUID `json:"id"`
 Artifact v2input.Ref `json:"artifact"`
 Digest string `json:"digest"`
 ItemCount int `json:"item_count"`
}
func cloneExportSnapshot(p CompiledExportSnapshot)CompiledExportSnapshot{return CompiledExportSnapshot{identity:p.identity,document:p.Body(),request:p.Request()}}

// Prepare cannot return a receipt before durable catalogue attachment. Failed
// attachment leaves an unreferenced immutable object for normal orphan cleanup.
func(p ExportSnapshotPreparation)Prepare(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,key string,raw []byte)(ExportSnapshotReceipt,error){
 var zero ExportSnapshotReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if p.Authority==nil||p.Publication==nil||p.Attachment==nil{return zero,v2input.ErrInvalid}
 compiled,err:=CompileExportSnapshotRequest(ctx,tenant,project,who,key,raw);if err!=nil{return zero,err}
 if err:=p.Authority.CheckExportSnapshot(ctx,who,tenant,project,cloneExportSnapshot(compiled));err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 root,err:=p.Publication.PublishExportSnapshot(ctx,who,tenant,project,key,cloneExportSnapshot(compiled));if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 if root.ArtifactID==uuid.Nil||root.SHA256!=rawSHA(compiled.document){return zero,v2input.ErrInvalid}
 snapshot,err:=v2input.DecodePinnedSessionInputSnapshot(root,compiled.document);if err!=nil{return zero,err}
 // Recompile from owned request before accepting a publication receipt.
 repeated,err:=CompileExportSnapshotRequest(ctx,tenant,project,who,key,compiled.request)
 if err!=nil||repeated.identity!=compiled.identity||!bytes.Equal(repeated.document,compiled.document){return zero,v2input.ErrInvalid}
 if err:=p.Authority.CheckExportSnapshot(ctx,who,tenant,project,cloneExportSnapshot(compiled));err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 if err:=p.Attachment.AttachExportSnapshot(ctx,who,tenant,project,key,cloneExportSnapshot(compiled),root);err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return ExportSnapshotReceipt{ID:compiled.identity,Artifact:root,Digest:snapshot.SnapshotDigest,ItemCount:len(snapshot.Items)},nil
}
