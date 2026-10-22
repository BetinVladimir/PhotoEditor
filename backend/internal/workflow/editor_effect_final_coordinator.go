package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

func(c *EditorEffectsCoordinator)processFinalOne(ctx context.Context)(worked bool,err error){
	if c.Calibrations==nil{return false,nil}
	var tenant,build uuid.UUID;var origin []byte;var cursor *uuid.UUID;if c.afterFinal!=uuid.Nil{cursor=&c.afterFinal}
	err=c.DB.QueryRow(ctx,`SELECT * FROM photo.editor_effect_final_context($1)`,cursor).Scan(&tenant,&build,&origin)
	if errors.Is(err,pgx.ErrNoRows){c.afterFinal=uuid.Nil;return false,nil};if err!=nil{return false,err};c.afterFinal=build
	defer func(){if err!=nil{worked,err=c.finishFinalPhysicalFailure(ctx,tenant,build,err)}}()
	physical:=*c;physical.Objects=editorFinalPhysicalObjects{EditorEffectsObjects:c.Objects}
	prepared,err:=physical.prepareFinalFreeze(ctx,tenant,build,origin);if err!=nil{return false,err}
	plan,err:=physical.compileFinalRender(ctx,prepared);if err!=nil{return false,err}
	pin,err:=canonical.Marshal(plan.Qualification);if err!=nil{return false,err};var accepted bool
	err=c.DB.QueryRow(ctx,`SELECT photo.begin_editor_effect_final_render($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`,tenant,build,prepared.Run,prepared.Workflow,string(plan.Compiled.Config),plan.PolicySHA,plan.Compiled.OutputBytes,string(pin)).Scan(&accepted)
	if err!=nil{return false,err};if !accepted{return false,artifactjson.ErrStale}
	evidence,sourceRaw,err:=physical.resolveFinalEvidence(ctx,prepared);if err!=nil{return false,err}
	resolver:=editorFinalInventory{prepared:prepared,coordinator:&physical,entries:evidence.Entries}
	transport:=v2render.RenderTransportPublisher{Documents:artifactjson.Publisher{Repository:artifactjson.SQLRepository{DB:c.DB,Bucket:c.Bucket},Objects:c.Objects}}
	graph,err:=transport.Prepare(ctx,prepared.Catalog.Actor,tenant,prepared.Run,prepared.Task,prepared.Receipt.ProcessedSource,prepared.Recipe,prepared.RecipeRaw,sourceRaw,resolver);if err!=nil{return false,err}
	if len(plan.Compiled.Tasks)!=1{return false,v2input.ErrInvalid}
	if err:=(v2render.RenderInputAdmission{DB:c.DB}).RegisterRenderInputs(ctx,tenant,prepared.Run,prepared.Workflow,0,plan.Compiled.Tasks[0],graph);err!=nil{return false,err}
	err=c.DB.QueryRow(ctx,`SELECT photo.activate_editor_effect_final_render($1,$2,$3)`,tenant,build,prepared.Run).Scan(&accepted)
	if err!=nil{return false,err};if err:=ctx.Err();err!=nil{return false,err};if !accepted{return false,artifactjson.ErrStale};return true,nil
}

type editorFinalInventory struct{prepared editorPreparedFinalFreeze;coordinator *EditorEffectsCoordinator;entries []v2input.Entry}
func(r editorFinalInventory)ResolveTaskInputs(ctx context.Context,actor auth.Identity,tenant uuid.UUID,roots []v2input.Entry,binding v2input.SessionVersionBinding)([]v2input.Entry,error){
	if err:=ctx.Err();err!=nil{return nil,err}
	if actor.UserID!=r.prepared.Catalog.Actor.UserID||actor.SessionID!=r.prepared.Catalog.Actor.SessionID||tenant!=r.prepared.Catalog.Tenant||binding!=r.prepared.Receipt.ProcessedSource||len(roots)!=2{return nil,v2input.ErrInvalid}
	if !validEditorFinalRoots(roots,r.prepared.Recipe,binding.Source){return nil,v2input.ErrInvalid}
	if _,err:=r.prepared.Catalog.Lookup(ctx,actor,tenant,v2draft.LayerRef{Ref:r.prepared.Recipe,Contract:"FrozenRenderRecipe"});err!=nil{return nil,err}
	return append([]v2input.Entry(nil),r.entries...),nil
}
func validEditorFinalRoots(roots []v2input.Entry,recipe,source v2input.Ref)bool{
 if len(roots)!=2{return false};recipeCount,sourceCount:=0,0
 for _,root:=range roots{switch{case root.Ref==recipe&&root.Contract=="FrozenRenderRecipe":recipeCount++;case root.Ref==source&&root.Contract=="WorkingImage":sourceCount++;default:return false}}
 return recipeCount==1&&sourceCount==1
}
