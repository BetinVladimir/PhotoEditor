package workflow

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

type editorFinalRenderPlan struct{Compiled v2render.CompiledRenderRun;Qualification v2render.RenderQualificationPin;PolicySHA string}

func(c *EditorEffectsCoordinator)compileFinalRender(ctx context.Context,prepared editorPreparedFinalFreeze)(editorFinalRenderPlan,error){
	var result editorFinalRenderPlan
	if c.Calibrations==nil{return result,v2input.ErrInvalid}
	frozen,err:=v2draft.DecodeFrozenRenderRecipe(prepared.RecipeRaw);if err!=nil{return result,err}
	evidence,sourceRaw,err:=c.resolveFinalEvidence(ctx,prepared);if err!=nil{return result,err}
	var qualification v2render.RenderQualification
	err=c.DB.QueryRow(ctx,`SELECT * FROM photo.editor_effect_final_qualification($1,$2)`,prepared.Catalog.Tenant,prepared.Origin.Run).Scan(&qualification.ModelSHA,&qualification.RuntimeSHA,&qualification.Backend,&qualification.DeviceSHA,&qualification.DatasetSHA)
	if err!=nil{return result,err};if qualification.ModelSHA!=frozen.Scalars.ModelLockSHA{return result,v2input.ErrInvalid}
	calibration,err:=c.Calibrations.Resolve(ctx,qualification);if err!=nil{return result,err}
	var image struct{Canvas struct{Width,Height int64}};if json.Unmarshal(sourceRaw,&image)!=nil{return result,v2input.ErrInvalid}
	choice,err:=calibration.Select(qualification,image.Canvas.Width,image.Canvas.Height,frozen.Scalars.ICCLockSHA);if err!=nil{return result,err}
	calibrationSHA,err:=canonical.Hash(calibration);if err!=nil{return result,err}
	request:=v2render.RenderDraftRequest{Tenant:prepared.Catalog.Tenant,Actor:prepared.Catalog.Actor,Binding:prepared.Receipt.ProcessedSource,RecipeRef:prepared.Recipe,RecipeRaw:prepared.RecipeRaw,SourceRaw:sourceRaw,ClosureDocuments:evidence.Documents,ClosureOccurrences:evidence.Occurrences,Backend:choice.Backend,RuntimeSHA:choice.RuntimeSHA,TileSize:choice.TileSize,AlphaMode:choice.AlphaMode,Provider:choice.Provider}
	result.Compiled,err=v2render.CompileRenderRunPlan(prepared.Run,prepared.Origin.Project,prepared.Origin.Snapshot,[]v2render.RenderDraftRequest{request},[][]v2input.Entry{evidence.Entries});if err!=nil{return result,err}
	result.Qualification=v2render.RenderQualificationPin{RenderQualification:qualification,CalibrationSHA:calibrationSHA}
	policy,err:=v2render.DefaultRenderPolicy();if err!=nil{return result,err};result.PolicySHA=policy.PolicySHA
	return result,nil
}
