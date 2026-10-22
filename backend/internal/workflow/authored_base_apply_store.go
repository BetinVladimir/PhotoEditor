package workflow

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// AuthoredBaseApplyStore keeps the real ProcessOne compiler, persisted reviews
// and quality checks. Marked children never reach ordinary collection adoption.
type AuthoredBaseApplyStore struct {ApplyStore;Objects EditorEffectsObjects;Bucket string}
func(s AuthoredBaseApplyStore)Adopt(ctx context.Context,l ApplyLease)(bool,error){
 if s.DB==nil{return false,objectstore.ErrInvalid}
 var marked bool;var parent *uuid.UUID;var raw []byte
 err:=s.DB.QueryRow(ctx,`SELECT * FROM photo.authored_render_base_adoption_context($1,$2,$3)`,l.TenantID,l.RunID,l.Token).Scan(&marked,&parent,&raw)
 if err!=nil{return false,err};if !marked{return s.ApplyStore.Adopt(ctx,l)}
 if parent==nil||*parent==uuid.Nil||s.Objects==nil{return false,objectstore.ErrIntegrity}
 body,err:=buildAuthoredBaseReceipt(ctx,s,l,*parent,raw);if err!=nil{return false,err}
 _,err=(AuthoredBasePublication{DB:s.DB,Objects:s.Objects,Bucket:s.Bucket}).Complete(ctx,l,*parent,body)
 return err==nil,err
}

type authoredBaseStage struct {
 Ordinal int `json:"ordinal"`;Phase string `json:"phase"`;Operation string `json:"operation"`
 Task uuid.UUID `json:"task_id"`;Result uuid.UUID `json:"result_id"`
 Attempt int `json:"attempt"`;Fence int64 `json:"fence"`
 Params review.ArtifactRef `json:"params"`;InputDigest string `json:"input_digest"`
 ProfileSHA string `json:"execution_profile_sha256"`;ProfileJCS string `json:"execution_profile_jcs"`
 Outputs []review.ArtifactRef `json:"outputs"`
}
type authoredBaseReceipt struct {
 Schema int `json:"schema_version"`;Tenant uuid.UUID `json:"tenant_id"`;Parent uuid.UUID `json:"parent_run_id"`;Base uuid.UUID `json:"base_run_id"`
 Recipe v2input.Ref `json:"original_recipe"`;Original v2input.SessionVersionBinding `json:"original_source"`;Processed v2input.SessionVersionBinding `json:"processed_source"`
 Style v2input.Ref `json:"effective_style"`;PlanSHA string `json:"phase_plan_sha256"`;Stages []authoredBaseStage `json:"stages"`
 Quality review.ArtifactRef `json:"quality"`;Generated *review.ArtifactRef `json:"generated_coverage"`;Inherited bool `json:"inherited_review"`
}
func buildAuthoredBaseReceipt(ctx context.Context,s AuthoredBaseApplyStore,l ApplyLease,parent uuid.UUID,raw []byte)([]byte,error){
 invalid:=func()([]byte,error){return nil,objectstore.ErrIntegrity}
 if len(raw)<2||len(raw)>1048576{return invalid()};var body authoredBaseReceipt
 if json.Unmarshal(raw,&body)!=nil||body.Schema!=1||body.Tenant!=l.TenantID||body.Parent!=parent||body.Base!=l.RunID||parent==l.RunID||len(body.Stages)<1||len(body.Stages)>5000||!body.Inherited{return invalid()}
 if body.Original.AssetVersionID==uuid.Nil||body.Original.AssetVersionID!=body.Processed.AssetVersionID{return invalid()}
 seen:=map[uuid.UUID]bool{}
 for i:=range body.Stages{step:=&body.Stages[i]
  if step.Ordinal!=i||step.Task==uuid.Nil||step.Result==uuid.Nil||step.Attempt<1||step.Fence<1||seen[step.Task]||step.Operation=="editor.layers_precompose"||len(step.Outputs)==0||len(step.Outputs)>128{return invalid()};seen[step.Task]=true
  profile,e:=canonical.Bytes([]byte(step.ProfileJCS));if e!=nil{return nil,e};step.ProfileJCS=string(profile)
  step.ProfileSHA,e=canonical.Hash(json.RawMessage(profile));if e!=nil{return nil,e}
 }
 read:=func(ref review.ArtifactRef)([]byte,error){a,e:=s.Artifact(ctx,l.TenantID,ref);if e!=nil{return nil,e};return readApplyJSON(ctx,s.Objects,s.Bucket,a)}
 image:=func(ref v2input.Ref)(review.WorkingImage,error){b,e:=read(review.ArtifactRef{ID:ref.ArtifactID,SHA256:ref.SHA256});if e!=nil{return review.WorkingImage{},e};return review.ParseWorkingImage(b)}
 original,err:=image(body.Original.Source);if err!=nil{return nil,err};processed,err:=image(body.Processed.Source);if err!=nil{return nil,err}
 originalCanvas,err:=canonical.Hash(original.Canvas);if err!=nil{return nil,err};processedCanvas,err:=canonical.Hash(processed.Canvas);if err!=nil{return nil,err}
 if originalCanvas!=body.Original.CanvasHash||(body.Processed.CanvasHash!=""&&processedCanvas!=body.Processed.CanvasHash)||original.Color!=processed.Color||original.Canvas.SourceSHA256!=processed.Canvas.SourceSHA256{return invalid()}
 body.Processed.CanvasHash=processedCanvas
 quality,err:=read(body.Quality);if err!=nil{return nil,err}
 var q struct{Target review.ArtifactRef `json:"target"`;Kind string `json:"target_kind"`;Stage string `json:"stage"`;Status string `json:"status"`;Technical struct{Finite bool `json:"finite"`} `json:"technical"`}
 if json.Unmarshal(quality,&q)!=nil||q.Target!=(review.ArtifactRef{ID:body.Processed.Source.ArtifactID,SHA256:body.Processed.Source.SHA256})||q.Kind!="working"||q.Stage!="master"||!q.Technical.Finite||(q.Status!="pass"&&q.Status!="review"){return invalid()}
 if body.Generated!=nil{b,e:=read(*body.Generated);if e!=nil{return nil,e};mask,e:=review.ParseMaskRef(b);if e!=nil||mask.Protected||mask.Selector!="generated"||mask.Canvas!=processed.Canvas{return invalid()}}
 if err=ctx.Err();err!=nil{return nil,err};result,err:=canonical.Marshal(body);if err!=nil{return nil,err};if len(result)>1048576{return invalid()};return result,nil
}
var _ ApplyPhaseStore=AuthoredBaseApplyStore{}
