package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/v2input"
)

// ExportSnapshotRequest pins both the source and the exact approved recipe.
// Neither a latest-recipe lookup nor a bare catalogue snapshot is sufficient.
type ExportSnapshotRequest struct {
 Items []v2input.SessionSnapshotItem `json:"items"`
}

type CompiledExportSnapshot struct {
 identity uuid.UUID
 document []byte
 request []byte
}
func(p CompiledExportSnapshot)ID()uuid.UUID{return p.identity}
func(p CompiledExportSnapshot)Body()[]byte{return append([]byte(nil),p.document...)}
func(p CompiledExportSnapshot)Request()[]byte{return append([]byte(nil),p.request...)}

// CompileExportSnapshotRequest only validates syntax and freezes ownership.
// Publication MUST separately prove source/recipe closure and catalogue scope,
// then atomically attach the verified object to this snapshot identity.
func CompileExportSnapshotRequest(ctx context.Context,tenant,project uuid.UUID,who auth.Identity,key string,raw []byte)(CompiledExportSnapshot,error){
 var zero CompiledExportSnapshot
 if err:=ctx.Err();err!=nil{return zero,err}
 if tenant==uuid.Nil||project==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||!idempotency.ValidKey(key)||len(raw)<2||len(raw)>1<<20{return zero,v2input.ErrInvalid}
 var request ExportSnapshotRequest
 if err:=decodeImportDocument(append([]byte(nil),raw...),&request);err!=nil{return zero,err}
 id:=uuid.NewSHA1(tenant,[]byte("interchange:export-snapshot:"+project.String()+":"+who.UserID.String()+":"+key))
 _,body,err:=v2input.CompilePinnedSessionInputSnapshot(id,request.Items);if err!=nil{return zero,err}
 // CompilePinnedSessionInputSnapshot owns and validates all nested fields.
 snapshot,err:=v2input.DecodePinnedSessionInputSnapshot(v2input.Ref{ArtifactID:id,SHA256:rawSHA(body)},body);if err!=nil{return zero,err}
 owned,err:=canonical.Marshal(ExportSnapshotRequest{Items:snapshot.Items});if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return CompiledExportSnapshot{identity:id,document:append([]byte(nil),body...),request:owned},nil
}

// ValidateReceipt independently binds HTTP success to the submitted document.
func(p CompiledExportSnapshot)ValidateReceipt(receipt ExportSnapshotReceipt)error{
 root:=v2input.Ref{ArtifactID:uuid.NewSHA1(p.ID(),[]byte("interchange:export-snapshot:artifact:1")),SHA256:rawSHA(p.document)}
 snapshot,err:=v2input.DecodePinnedSessionInputSnapshot(root,p.document);if err!=nil{return err}
 if receipt.ID!=p.ID()||receipt.Artifact!=root||receipt.Digest!=snapshot.SnapshotDigest||receipt.ItemCount!=len(snapshot.Items){return v2input.ErrInvalid}
 return nil
}
