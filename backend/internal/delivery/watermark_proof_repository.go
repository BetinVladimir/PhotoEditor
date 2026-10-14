package delivery
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
// SaveWatermarkPrepareProof runs in the caller's photo_api admission transaction
// after all pins/Params have been persisted. SQL repeats current pin authority;
// compilation alone cannot authorize source access or dispatch.
func SaveWatermarkPrepareProof(ctx context.Context,tx ProofTransaction,who auth.Identity,scope PrepareProofScope,frozenRaw,snapshotRaw []byte,configs []PresetContent,pins []PreparedWatermarkPin)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||scope.Tenant==uuid.Nil||scope.Project==uuid.Nil||scope.Run==uuid.Nil||scope.Task==uuid.Nil||scope.Params.ArtifactID==uuid.Nil{return v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(scope.Run);if err!=nil{return err}
 task,err:=v2input.TaskID(workflow,"v2.delivery.prepare",0);if err!=nil||task!=scope.Task{return v2input.ErrInvalid}
 pinScope:=WatermarkPinScope{Tenant:scope.Tenant,Project:scope.Project,Actor:who.UserID,Parent:scope.Run}
 _,proof,params,err:=CompileWatermarkPrepareProof(pinScope,who.SessionID,scope.Frozen,frozenRaw,snapshotRaw,configs,pins);if err!=nil{return err}
 if digest(params)!=scope.Params.SHA256{return v2input.ErrInvalid}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.save_v30_delivery_prepare_proof($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb)`,scope.Tenant,who.UserID,who.SessionID,scope.Project,scope.Run,scope.Task,scope.Params.ArtifactID,scope.Params.SHA256,string(params),string(proof)).Scan(&accepted)
 if err!=nil{return err};if !accepted{return domain.NewError("DELIVERY_SOURCE_STALE",409)};return ctx.Err()
}
