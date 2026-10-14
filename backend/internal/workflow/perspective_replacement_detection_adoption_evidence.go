package workflow
import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
// ReplacementDetectionAdoptionEvidence requires a caller-owned transaction so
// physical source locks survive through subsequent result validation/commit.
// Returned bytes are SQL metadata evidence only, not verified result pixels.
func(r ReplacementDetectionDocuments) AdoptionEvidence(ctx context.Context,tx pgx.Tx,attempt int,fence int64,result uuid.UUID)([]byte,error){
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return nil,err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return nil,err}
 if tx==nil||attempt<1||attempt>6||fence<1||fence>9007199254740991||result==uuid.Nil{return nil,v2input.ErrInvalid}
 if err=replacementDetectionAdoptionTxScope(ctx,tx,scope.Tenant);err!=nil{return nil,err}
 step,proof,err:=r.Prepared.PerspectiveDetectionCurrentProof(ctx,tx,r.Rebuilt,r.Retained,r.Mappings,r.Unchanged,r.Plan.Descriptor(),r.Snapshot);if err!=nil{return nil,err}
 if _,err=RestorePerspectiveReplacementDetectionPlan(ctx,step,r.Plan.Descriptor());err!=nil{return nil,err}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.v29_replacement_detector_adoption_evidence($1,$2,$3,$4,$5,$6,$7::jsonb)",scope.Tenant,scope.Owner,attempt,fence,result,string(body),string(proof)).Scan(&raw);err!=nil{return nil,err}
 if len(raw)<2||len(raw)>262144{return nil,v2input.ErrExpired}
 if err=ctx.Err();err!=nil{return nil,err}
 return append([]byte(nil),raw...),nil
}
