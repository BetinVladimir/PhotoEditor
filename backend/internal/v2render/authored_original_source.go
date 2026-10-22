package v2render

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// LoadOriginalBinding keeps original raw-container attribution separate from
// the changed-canvas current WorkingImage. The existing catalog query is made
// with the immutable ORIGINAL master, never weakened to accept an intermediate.
// The final worker binding names the actual current source and the independently
// resolved original raw file, preserving the optical metadata ABI.
func(s SQLAuthoredPhaseObjects)LoadOriginalBinding(ctx context.Context,scheduler *pgxpool.Pool,who auth.Identity,
 tenantID,projectID,parent uuid.UUID,ordinal int)(v2input.LensOriginalBinding,error){
 fail:=func()(v2input.LensOriginalBinding,error){return v2input.LensOriginalBinding{},v2input.ErrInvalid}
 source,found,err:=ReadAuthoredInstructionSource(ctx,scheduler,tenantID,parent,ordinal)
 if err!=nil{return v2input.LensOriginalBinding{},err};if !found||source.Actor!=who.UserID||source.Session!=who.SessionID||source.Project!=projectID{return fail()}
 var frozen struct{Source v2input.SessionVersionBinding `json:"source"`}
 if json.Unmarshal([]byte(source.LayerStackJCS),&frozen)!=nil{return fail()}
 original,current:=frozen.Source,source.Source
 if !authoredBindingValid(original)||!authoredBindingValid(current)||original.AssetVersionID!=current.AssetVersionID{return fail()}
 originalObject,err:=s.Object(ctx,who,tenantID,projectID,parent,ordinal,original.Source);if err!=nil{return v2input.LensOriginalBinding{},err}
 currentRaw,err:=s.ReadJSON(ctx,who,tenantID,projectID,parent,ordinal,current.Source);if err!=nil{return v2input.LensOriginalBinding{},err}
 var image review.WorkingImage
 if json.Unmarshal(currentRaw,&image)!=nil{return fail()}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil||canvas!=current.CanvasHash{return fail()}
 var resolved v2input.LensOriginalSource
 err=tenant.WithTenant(ctx,s.API,who,tenantID,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if _,err:=project.Authorize(ctx,tx,tenantID,projectID,who.UserID,role,domain.Editor,false);err!=nil{return err}
  value,err:=(v2input.SQLLensOriginalSource{DB:tx,Bucket:s.Bucket,Objects:s.Objects}).Load(ctx,
   tenantID,projectID,who.UserID,who.SessionID,original,originalObject)
  if err!=nil{return err};resolved=value;return nil
 })
 if err!=nil{return v2input.LensOriginalBinding{},err}
 if resolved.Ref.SHA256!=image.Canvas.SourceSHA256{return fail()}
 // Repeat the locked parent's current source authorization after object IO and
 // independent original lookup; expiry/cancellation cannot be masked by bytes.
 latest,err:=s.Object(ctx,who,tenantID,projectID,parent,ordinal,current.Source);if err!=nil{return v2input.LensOriginalBinding{},err}
 if latest.SHA256!=current.Source.SHA256{return fail()}
 after,found,err:=ReadAuthoredInstructionSource(ctx,scheduler,tenantID,parent,ordinal)
 if err!=nil{return v2input.LensOriginalBinding{},err}
 if !found||after.Source!=current||after.PredecessorSHA!=source.PredecessorSHA||after.ProgramSHA!=source.ProgramSHA||after.RecipeJCS!=source.RecipeJCS||after.LayerStackJCS!=source.LayerStackJCS{return fail()}
 return v2input.CompileLensOriginalBinding(current,resolved)
}
