package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type editorFinalOrigin struct{
	Run uuid.UUID `json:"run_id"`
	Project uuid.UUID `json:"project_id"`
	Actor uuid.UUID `json:"actor_id"`
	Session uuid.UUID `json:"session_id"`
	Snapshot uuid.UUID `json:"input_snapshot_id"`
	RecipeID uuid.UUID `json:"frozen_recipe_artifact_id"`
	RecipeSHA string `json:"frozen_recipe_sha"`
	ReceiptID uuid.UUID `json:"seal_receipt_artifact_id"`
	ReceiptSHA string `json:"seal_receipt_sha"`
}
type editorPreparedFinalFreeze struct{
	Origin editorFinalOrigin
	Run,Workflow,Task uuid.UUID
	Recipe v2input.Ref
	RecipeRaw,ReceiptRaw []byte
	Receipt v2draft.EditorExecutedBaseReceipt
	Catalog editorFinalCatalog
}

func(c *EditorEffectsCoordinator)prepareFinalFreeze(ctx context.Context,tenant,build uuid.UUID,originRaw []byte)(editorPreparedFinalFreeze,error){
	var out editorPreparedFinalFreeze
	if json.Unmarshal(originRaw,&out.Origin)!=nil||out.Origin.Run!=build||out.Origin.Project==uuid.Nil||out.Origin.Actor==uuid.Nil||out.Origin.Session==uuid.Nil||out.Origin.Snapshot==uuid.Nil{return out,v2input.ErrInvalid}
	out.Catalog=editorFinalCatalog{DB:c.DB,Tenant:tenant,Build:build,Actor:auth.Identity{UserID:out.Origin.Actor,SessionID:out.Origin.Session},Bucket:c.Bucket}
	receiptRef:=v2input.Ref{ArtifactID:out.Origin.ReceiptID,SHA256:out.Origin.ReceiptSHA}
	receipt,receiptRaw,err:=readEditorFinalReceipt(ctx,c.Objects,out.Catalog,receiptRef);if err!=nil{return out,err};out.Receipt=receipt;out.ReceiptRaw=receiptRaw
	originalRef:=v2input.Ref{ArtifactID:out.Origin.RecipeID,SHA256:out.Origin.RecipeSHA};if receipt.OriginalRecipe!=originalRef{return out,v2input.ErrInvalid}
	originalObject,err:=out.Catalog.Lookup(ctx,out.Catalog.Actor,tenant,v2draft.LayerRef{Ref:originalRef,Contract:"FrozenRenderRecipe"});if err!=nil{return out,err}
	originalRaw,err:=readEditorFinalDocument(ctx,c.Objects,originalObject,1<<20);if err!=nil{return out,err}
	out.RecipeRaw,err=v2draft.BuildEditorFinalRecipe(originalRaw,originalRef,receiptRef,receiptRaw,build);if err!=nil{return out,err}
	var accepted bool;err=c.DB.QueryRow(ctx,`SELECT photo.prepare_editor_effect_final_recipe($1,$2,$3)`,tenant,build,string(out.RecipeRaw)).Scan(&accepted);if err!=nil{return out,err};if !accepted{return out,artifactjson.ErrStale}
	out.Run=uuid.NewSHA1(build,[]byte("editor.effects_final/run"));out.Workflow,err=v2input.WorkflowID(out.Run);if err!=nil{return out,err};out.Task,err=v2input.TaskID(out.Workflow,"render.compose",0);if err!=nil{return out,err}
	sha,err:=canonical.Hash(json.RawMessage(out.RecipeRaw));if err!=nil{return out,err}
	scope:=artifactjson.Scope{Tenant:tenant,Run:build,Owner:out.Task,Purpose:"task_params"}
	published,err:=(artifactjson.Publisher{Repository:editorFinalDocuments{DB:c.DB,Scope:scope,Bucket:c.Bucket,SHA:sha,Bytes:int64(len(out.RecipeRaw))},Objects:c.Objects}).Publish(ctx,scope,out.RecipeRaw);if err!=nil{return out,err}
	out.Recipe=v2input.Ref{ArtifactID:published.ID,SHA256:published.SHA256};return out,nil
}
