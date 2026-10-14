package workflow

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func replacementDetectionAdoptionTxScope(ctx context.Context,tx pgx.Tx,tenant uuid.UUID)error{
 if err:=ctx.Err();err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 var role,currentTenant string
 if err:=tx.QueryRow(ctx,"SELECT current_user,coalesce(current_setting('app.tenant_id',true),'')").Scan(&role,&currentTenant);err!=nil{return err}
 if role!="photo_scheduler"||currentTenant!=tenant.String(){return v2input.ErrInvalid};return nil
}

// CommitAdoption is the SQL boundary for a trusted scheduler's pinned replay
// result. The caller must perform pinned replay before calling it and rollback
// on any error. This method retains fresh physical mapping locks through commit;
// it neither performs replay nor commits the caller-owned transaction itself.
func(r ReplacementDetectionDocuments) CommitAdoption(ctx context.Context,tx pgx.Tx,attempt int,fence int64,result uuid.UUID,inventory []byte,digest string,evidence,root []byte)error{
 scope,params,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(params)));err!=nil{return err}
 if tx==nil||attempt<1||attempt>6||fence<1||fence>9007199254740991||result==uuid.Nil||domainDigestInvalid(digest){return v2input.ErrInvalid}
 for _,item:=range []struct{raw []byte;limit int}{{inventory,8<<20},{evidence,262144},{root,1048576}}{
  if len(item.raw)<2||len(item.raw)>item.limit{return v2input.ErrInvalid}
  normal,err:=canonical.Bytes(item.raw);if err!=nil||!bytes.Equal(normal,item.raw){return v2input.ErrInvalid}
 }
 var binding struct{
  Run uuid.UUID `json:"run_id"`;Task uuid.UUID `json:"task_id"`;Result uuid.UUID `json:"result_id"`
  Attempt int `json:"attempt"`;Fence int64 `json:"fence"`
  Root v2input.Ref `json:"root"`;Params v2input.Ref `json:"params_ref"`
 }
 if json.Unmarshal(evidence,&binding)!=nil||binding.Run!=scope.Run||binding.Task!=scope.Owner||binding.Result!=result||binding.Attempt!=attempt||binding.Fence!=fence||binding.Root.ArtifactID==uuid.Nil||binding.Root.SHA256!=renderDerivativeBytesSHA(root)||binding.Params.ArtifactID!=task.Params.ID||binding.Params.SHA256!=task.Params.SHA256{return v2input.ErrInvalid}
 if err=replacementDetectionAdoptionTxScope(ctx,tx,scope.Tenant);err!=nil{return err}
 step,proof,err:=r.Prepared.PerspectiveDetectionCurrentProof(ctx,tx,r.Rebuilt,r.Retained,r.Mappings,r.Unchanged,r.Plan.Descriptor(),r.Snapshot);if err!=nil{return err}
 if _,err=RestorePerspectiveReplacementDetectionPlan(ctx,step,r.Plan.Descriptor());err!=nil{return err}
 var accepted bool
 if err=tx.QueryRow(ctx,"SELECT photo.adopt_v29_replacement_detector($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11::jsonb)",scope.Tenant,scope.Owner,attempt,fence,result,string(inventory),digest,string(evidence),string(params),string(root),string(proof)).Scan(&accepted);err!=nil{return err}
 if !accepted{return artifactjson.ErrStale};return ctx.Err()
}
