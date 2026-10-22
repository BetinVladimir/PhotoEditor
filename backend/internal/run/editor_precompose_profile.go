package run

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2render"
 "photoeditor/backend/internal/v2input"
)

func(s EditorEffectsService)precomposeProfile(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,req EditorEffectRequest,loader v2draft.RenderRecipeSource,loaded v2draft.LoadedRenderRecipe)(json.RawMessage,error){
 if len(loaded.LayerBody)==0{return nil,nil}
 unavailable:=domain.NewError("RENDER_UNAVAILABLE",503)
 if s.Calibrations==nil{return nil,unavailable}
 var pin v2render.RenderQualificationPin
 var icc string
 err:=tenant.WithTenant(ctx,s.Runs.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if _,e:=project.Authorize(ctx,tx,tid,pid,who.UserID,role,domain.Editor,false);e!=nil{return e}
  var body []byte
  if e:=tx.QueryRow(ctx,`SELECT photo.editor_precompose_installation_pin($1,$2,$3,$4)`,tid,pid,who.UserID,who.SessionID).Scan(&body);e!=nil{return e}
  var value struct{Pin v2render.RenderQualificationPin `json:"qualification_pin"`;ICC string `json:"icc_lock_sha"`}
  if len(body)==0||json.Unmarshal(body,&value)!=nil{return unavailable};pin=value.Pin;icc=value.ICC;return nil
 });if err!=nil{return nil,err}
 if pin.Backend!="cpu"||icc!=loaded.Inputs.ICCLockSHA{return nil,unavailable}
 calibration,err:=s.Calibrations.Resolve(ctx,pin.RenderQualification);if err!=nil{return nil,err}
 sha,err:=canonical.Hash(calibration);if err!=nil||sha!=pin.CalibrationSHA{return nil,unavailable}
 sourceRaw,err:=loader.Resolver.ReadLocalPortraitSource(ctx,who,tid,req.Source);if err!=nil{return nil,err}
 source,err:=review.ParseWorkingImage(sourceRaw);if err!=nil{return nil,err}
 choice,err:=calibration.Select(pin.RenderQualification,source.Canvas.Width,source.Canvas.Height,icc);if err!=nil{return nil,err}
 if choice.Provider.VRAMBytes!=0{return nil,unavailable}
 plan,err:=v2render.Compile(loaded.LayerBody,req.Source);if err!=nil{return nil,err}
 // The existing plan budget uses the same active mask graph/ref deduplication
 // as Python prepare_render_pixels; the frozen recipe retains protection.
 var recipe struct{Protected *json.RawMessage `json:"protected_mask"`}
 if json.Unmarshal(loaded.Authored,&recipe)!=nil{return nil,unavailable}
 var protected *v2input.Ref
 if recipe.Protected!=nil{var ref v2input.Ref;if json.Unmarshal(*recipe.Protected,&ref)!=nil{return nil,unavailable};protected=&ref}
 budget,err:=plan.BudgetRender(source.Canvas.Width,source.Canvas.Height,protected,choice.TileSize,choice.AlphaMode,choice.Provider);if err!=nil{return nil,err}
 return canonical.Marshal(map[string]any{"backend":"cpu","precision":"float32","tile_size":choice.TileSize,"alpha_mode":choice.AlphaMode,"resource_estimate":budget.Resources,"native_qualification_pin":pin,"icc_lock_sha":icc})
}
