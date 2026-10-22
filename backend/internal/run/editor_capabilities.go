package run

import (
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2render"
)

type EditorCapabilities struct {
 SchemaVersion int `json:"schema_version"`
 TenantID uuid.UUID `json:"tenant_id"`
 ProjectID uuid.UUID `json:"project_id"`
 AssetID uuid.UUID `json:"asset_id"`
 DocumentID uuid.UUID `json:"document_id"`
 DraftRevision string `json:"draft_revision"`
 Source v2input.SessionVersionBinding `json:"source"`
 CompilationSHA string `json:"compilation_sha"`
 Precompose bool `json:"precompose"`
 Available bool `json:"available"`
 Reason *string `json:"reason"`
 Backends []string `json:"backends"`
 ExpiresAt time.Time `json:"expires_at"`
}
// EditorCapabilitiesService reads the exact acknowledged bridge document. It
// neither freezes/publishes artifacts nor creates tasks/receipts. Actual future
// output dimensions/resources are qualified again by final render admission.
type EditorCapabilitiesService struct {Runs Service;Drafts v2draft.Service;Qualification v2render.RenderFreezeQualificationResolver;Calibrations v2render.RenderCalibrationCatalog}
func(s EditorCapabilitiesService)Read(ctx context.Context,who auth.Identity,tid,asset uuid.UUID,req v2render.RenderFreezeRequest)(out EditorCapabilities,err error){
 revision,e:=parseApplyCapabilityRevision(req.ExpectedRevision);if e!=nil||tid==uuid.Nil||asset==uuid.Nil||req.DocumentID==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 if req.AssumeProfile!=nil&&*req.AssumeProfile!="srgb_v1"&&*req.AssumeProfile!="linear_srgb_v1"&&*req.AssumeProfile!="work_rec2020_gamma24_v1"{return out,domain.NewError("VALIDATION_FAILED",422)}
 ctx,cancel:=context.WithTimeout(ctx,8*time.Second);defer cancel()
 draft,e:=s.Drafts.ReadApprovalDraft(ctx,who,tid,asset,req.DocumentID,revision);if e!=nil{return out,e};if draft.Revision!=revision{return out,domain.NewError("REVISION_CONFLICT",409)}
 out=EditorCapabilities{SchemaVersion:1,TenantID:tid,ProjectID:draft.ProjectID,AssetID:asset,DocumentID:req.DocumentID,DraftRevision:req.ExpectedRevision,Source:draft.Binding,Backends:[]string{}}
 // Qualification is the same current policy used by new Freeze, including the
 // installed measured calibration catalog required by the later composition.
 if s.Qualification==nil||s.Calibrations==nil{return out,domain.NewError("RENDER_UNAVAILABLE",503)}
 qualification,e:=s.Qualification.Resolve(ctx,who,tid,draft.ProjectID);if e!=nil{return out,e}
 if _,e=s.Calibrations.Resolve(ctx,qualification);e!=nil{return out,e}
 loader:=v2draft.RenderRecipeSource{Resolver:v2draft.ClosureResolver{Catalog:v2draft.SQLArtifactCatalog{DB:s.Drafts.DB,Bucket:s.Drafts.Bucket,ProjectID:draft.ProjectID,AssetVersionID:draft.AssetVersionID},Objects:s.Drafts.Objects},Learning:s.Drafts,ProjectID:draft.ProjectID}
 loaded,e:=loader.Load(ctx,who,tid,draft.DocumentID,draft.Recipe,draft.Binding,draft.Image.Color.ICCSHA256,qualification.ModelSHA);if e!=nil{return out,e}
 // This method is a read-only compiler: its background resolver performs SELECT
 // authority checks; it does not call PutImmutable or pinFreezePending.
 _,e=(v2draft.Publisher{DB:s.Drafts.DB,Objects:s.Drafts.Objects,Bucket:s.Drafts.Bucket}).CompileSelectedFrozenRenderRecipe(ctx,who,tid,draft.ProjectID,loaded.Authored,loaded.Inputs,loaded.LayerBody,loaded.StyleBody,loaded.OverrideBodies,loaded.SourceSHA,req.AssumeProfile);if e!=nil{return out,e}
 compilation,e:=v2draft.CompileEditorEffects(loaded.Authored,loaded.Inputs,loaded.StyleBody,loaded.OverrideBodies,loaded.SourceSHA);if e!=nil{return out,e}
 compilation,e=loader.Resolver.BindEditorEffectProtection(ctx,who,tid,loaded.Authored,draft.Binding,loaded.SourceSHA,compilation);if e!=nil{return out,e}
 out.CompilationSHA,e=canonical.Hash(compilation);if e!=nil{return out,e};out.Precompose=len(loaded.LayerBody)>0
 var needs styleExecutionNeeds;if json.Unmarshal(compilation.Style,&needs)!=nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 if e=mergeOverrideNeeds(&needs,compilation.Override);e!=nil{return out,e}
 if !applyMaskingModelsPresent(needs){return out,domain.NewError("STYLE_MODEL_BINDING_MISSING",409)}
 if e=validateApplyModuleDependencies(needs);e!=nil{return out,e};if e=validateApplyRegionBindings(needs.Extensions.RegionAdjustments,compilation.Override,needs.Geometry.Enabled);e!=nil{return out,e}
 err=tenant.WithTenant(ctx,s.Runs.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  var now time.Time;if e:=tx.QueryRow(ctx,`SELECT clock_timestamp()`).Scan(&now);e!=nil{return e};out.ExpiresAt=now.Add(15*time.Second)
  selection,e:=selectApplyRuntimes(ctx,tx,tid,needs,false);if e!=nil{code:=domain.AsError(e).Code;switch code{case "PERSON_MASK_CAPABILITY_UNAVAILABLE","RESTORATION_CAPABILITY_UNAVAILABLE","CREATIVE_CAPABILITY_UNAVAILABLE":out.Reason=&code;return nil};return e}
  operations:=[]string{"editor.effects_seal"};if out.Precompose{operations=append(operations,"editor.layers_precompose")}
  for _,operation:=range operations{var ready bool;e=tx.QueryRow(ctx,`SELECT photo.editor_capability_ready($1,$2,$3,$4,$5)`,tid,draft.ProjectID,who.UserID,who.SessionID,operation).Scan(&ready);if e!=nil{return e};if !ready{code:="EDITOR_SEAL_CAPABILITY_UNAVAILABLE";if operation=="editor.layers_precompose"{code="EDITOR_PRECOMPOSE_CAPABILITY_UNAVAILABLE"};out.Reason=&code;return nil}}
  for _,backend:=range []string{selection.PersonBackend,selection.RestorationBackend,selection.CreativeBackend,"cpu",qualification.Backend}{if backend!=""{seen:=false;for _,old:=range out.Backends{seen=seen||old==backend};if !seen{out.Backends=append(out.Backends,backend)}}};out.Available=true;return nil
 });if err!=nil{return out,err}
 // Repeat the production scoped reader after asynchronous compilation. It
 // rejects archived/revoked projects and changed binding/current draft.
 latest,e:=s.Drafts.ReadApprovalDraft(ctx,who,tid,asset,req.DocumentID,revision);if e!=nil{return out,e};if latest.Revision!=revision||latest.Binding!=draft.Binding||latest.ProjectID!=draft.ProjectID{return out,domain.NewError("REVISION_CONFLICT",409)}
 return out,nil
}
