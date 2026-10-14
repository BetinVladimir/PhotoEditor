package interchange

import (
 "bytes"
 "context"
 "errors"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// Reservation must bind tenant/project/principal/key, canonical request hash,
// stable artifact identity, byte quota and a fencing token in durable storage.
// A ready receipt is valid only while the corresponding versioned object remains
// verified and retained. Complete repeats those conditions under the token.
type ExportSnapshotObjectRepository interface {
 ReserveExportSnapshot(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,CompiledExportSnapshot)(artifactjson.Reservation,error)
 CompleteExportSnapshot(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,CompiledExportSnapshot,artifactjson.Reservation,objectstore.Object)(v2input.Ref,error)
}
type ExportSnapshotObjectPublisher struct {
 Repository ExportSnapshotObjectRepository
 Objects artifactjson.Objects
}
func(p ExportSnapshotObjectPublisher)PublishExportSnapshot(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,key string,compiled CompiledExportSnapshot)(v2input.Ref,error){
 var zero v2input.Ref
 if err:=ctx.Err();err!=nil{return zero,err}
 if p.Repository==nil||p.Objects==nil{return zero,v2input.ErrInvalid}
 expected,err:=CompileExportSnapshotRequest(ctx,tenant,project,who,key,compiled.Request());if err!=nil{return zero,err}
 if expected.ID()!=compiled.ID()||!bytes.Equal(expected.Body(),compiled.Body()){return zero,v2input.ErrInvalid}
 artifact:=uuid.NewSHA1(expected.ID(),[]byte("interchange:export-snapshot:artifact:1"))
 root:=v2input.Ref{ArtifactID:artifact,SHA256:rawSHA(expected.document)}
 reservation,err:=p.Repository.ReserveExportSnapshot(ctx,who,tenant,project,key,cloneExportSnapshot(expected));if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 if reservation.ArtifactID!=artifact||reservation.Token==uuid.Nil{return zero,objectstore.ErrIntegrity}
 if reservation.Ready!=nil{
  if reservation.Ready.ID!=artifact||reservation.Ready.SHA256!=root.SHA256{return zero,objectstore.ErrIntegrity}
  return root,ctx.Err()
 }
 objectKey,err:=objectstore.DocumentKey(tenant,expected.ID(),artifact);if err!=nil{return zero,err}
 size:=int64(len(expected.document))
 object,err:=p.Objects.PutImmutable(ctx,objectKey,bytes.NewReader(expected.Body()),size,root.SHA256,"application/json")
 if err:=ctx.Err();err!=nil{return zero,err}
 if errors.Is(err,objectstore.ErrExists)||errors.Is(err,objectstore.ErrUnavailable){object,err=p.Objects.RecoverImmutable(ctx,objectKey,size,root.SHA256,"application/json")}
 if err!=nil{return zero,err};if err:=ctx.Err();err!=nil{return zero,err}
 if object.Key!=objectKey||object.Bytes!=size||object.SHA256!=root.SHA256||object.ContentType!="application/json"||object.VersionID==""||object.VersionID=="null"{return zero,objectstore.ErrIntegrity}
 actual,err:=p.Repository.CompleteExportSnapshot(ctx,who,tenant,project,key,cloneExportSnapshot(expected),reservation,object)
 if err!=nil{return zero,err};if err:=ctx.Err();err!=nil{return zero,err}
 if actual!=root{return zero,objectstore.ErrIntegrity}
 return root,nil
}
var _ ExportSnapshotPublication=ExportSnapshotObjectPublisher{}
