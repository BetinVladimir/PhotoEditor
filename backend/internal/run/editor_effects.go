package run

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2render"
)

type EditorEffectsService struct {Runs Service;Drafts v2draft.Service; FreshRenderInstalled bool; Calibrations v2render.RenderCalibrationCatalog; AuthoredCalibrations v2render.AuthoredFinalCalibrationCatalog}

// Begin reads the original frozen document closure and compiles all requested
// modules before entering the existing Apply admission/worker/review engine.
// Mutable client parameters are never accepted as executable stage evidence.
func(s EditorEffectsService)Begin(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,key string,req EditorEffectRequest)(idempotency.Response,error){
 return s.begin(ctx,who,tid,pid,key,req,nil)
}
func(s EditorEffectsService)begin(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,key string,req EditorEffectRequest,baseOrigin *authoredBaseApplyBinding)(idempotency.Response,error){
 invalid:=func()(idempotency.Response,error){return idempotency.Response{},domain.NewError("VALIDATION_FAILED",422)}
 if s.Runs.DB==nil{return idempotency.Response{},domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 if tid==uuid.Nil||pid==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||req.Source.AssetVersionID==uuid.Nil||req.FrozenDocumentID==uuid.Nil||req.DraftDocumentID==uuid.Nil||req.DraftRevision<1||req.DraftRevision>9007199254740991||!idempotency.ValidKey(key){return invalid()}
 if _,err:=domain.ParseDigest(req.Source.CanvasHash);err!=nil{return invalid()};for _,ref:=range []v2input.Ref{req.Source.Source,req.Recipe}{if ref.ArtifactID==uuid.Nil||!isLowerHexSHA(ref.SHA256){return invalid()}}
 route:="Editor.Effects."+pid.String()
 if baseOrigin!=nil{route="V26.AuthoredBase."+pid.String()}
 var replay idempotency.Response;var found bool;var err error
 if baseOrigin==nil{replay,found,err=s.replay(ctx,who,tid,pid,key,route,req)}else{replay,found,err=s.replayAuthoredBase(ctx,who,tid,pid,key,route,req,*baseOrigin)}
 if err!=nil||found{return replay,err}
 // Live installation and object IO are fresh-admission dependencies, never
 // prerequisites for an authenticated immutable accepted receipt.
 if !s.FreshRenderInstalled{return idempotency.Response{},domain.NewError("RENDER_UNAVAILABLE",503)}
 if s.Drafts.DB==nil||s.Drafts.Objects==nil{return idempotency.Response{},domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 prepared,err:=s.prepareEditorEffects(ctx,who,tid,pid,req,baseOrigin,false)
 if err!=nil{return idempotency.Response{},err}
 mode,styleVersion:=prepared.Mode,prepared.StyleVersion
 return s.Runs.apply(ctx,who,tid,uuid.Nil,key,ApplyRequest{SourceRevision:req.DraftRevision,StyleVersionID:styleVersion,Scope:"selected",ItemIDs:[]uuid.UUID{mode.ItemID}},false,route,"",nil,nil,mode)
}
func(s EditorEffectsService)replay(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,key,route string,req EditorEffectRequest)(idempotency.Response,bool,error){
 var out idempotency.Response;found:=false;hash,err:=canonical.Hash(req);if err!=nil{return out,false,err}
 err=tenant.WithTenant(ctx,s.Runs.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if _,e:=project.Authorize(ctx,tx,tid,pid,who.UserID,role,domain.Editor,false);e!=nil{return e}
  var stored string;var status *int;var body []byte;var resource *uuid.UUID
  e:=tx.QueryRow(ctx,`SELECT request_hash,response_status,response_body,resource_id FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4`,tid,who.UserID,route,key).Scan(&stored,&status,&body,&resource);if errors.Is(e,pgx.ErrNoRows){return nil};if e!=nil{return e};if stored!=hash{return domain.NewError("IDEMPOTENCY_CONFLICT",409)};if status==nil{return nil};if resource==nil{return domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
  var valid bool;e=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.editor_effect_builds o WHERE o.tenant_id=$1 AND o.run_id=$2 AND o.project_id=$3 AND o.actor_id=$4 AND o.asset_version_id=$5 AND o.source_artifact_id=$6 AND o.source_sha=$7 AND o.frozen_recipe_artifact_id=$8 AND o.frozen_recipe_sha=$9)`,tid,*resource,pid,who.UserID,req.Source.AssetVersionID,req.Source.Source.ArtifactID,req.Source.Source.SHA256,req.Recipe.ArtifactID,req.Recipe.SHA256).Scan(&valid);if e!=nil{return e};if !valid{return domain.NewError("SOURCE_VERSION_MISMATCH",409)}
  origin:=&editorEffectApplyMode{ProjectID:pid,Binding:req.Source,FrozenRecipe:req.Recipe,FrozenDocumentID:req.FrozenDocumentID,DraftDocumentID:req.DraftDocumentID,DraftRevision:req.DraftRevision,ItemID:uuid.NewSHA1(req.FrozenDocumentID,[]byte("editor-effect-source"))}
  if e:=origin.authorize(ctx,tx,who,tid,false);e!=nil{return e}
  // JSONB storage normalizes whitespace; reproduce the same canonical bytes as idempotency.Use.
  body,e=canonical.Bytes(body);if e!=nil{return e}
  if !validEditorEffectReceipt(*status,body,*resource){return domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
  var acceptedRun bool
  if e:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.runs WHERE tenant_id=$1 AND id=$2 AND kind='apply' AND created_by=$3 AND output_collection_id IS NULL)`,tid,*resource,who.UserID).Scan(&acceptedRun);e!=nil{return e};if !acceptedRun{return domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
  out=idempotency.Response{Status:*status,Body:body,ResourceID:resource,Replayed:true};found=true;return nil
 });return out,found,err
}

// Accepted Editor effects use the actual ApplyAccepted wire contract; the
// collection-free mode deliberately emits a nil output_collection_id.
func validEditorEffectReceipt(status int, body []byte, resource uuid.UUID) bool {
 if status!=202||resource==uuid.Nil{return false}
 var ticket ApplyAccepted
 decoder:=json.NewDecoder(bytes.NewReader(body));decoder.DisallowUnknownFields()
 if decoder.Decode(&ticket)!=nil{return false}
 var trailing any
 if decoder.Decode(&trailing)!=io.EOF{return false}
 // Canonical reproduction also refuses duplicate JSON fields and alternate
 // receipts while preserving the original bytes in the successful replay.
 expected,err:=canonical.Marshal(ticket)
 return err==nil&&bytes.Equal(expected,body)&&ticket.RunID==resource&&ticket.OutputCollectionID==uuid.Nil&&ticket.Status=="queued"
}
