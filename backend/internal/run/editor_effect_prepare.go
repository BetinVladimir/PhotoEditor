package run

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// One production loader is shared by ordinary Editor Begin, an actual base
// child and the authored parent compiler. No alternate accepted-style path.
type preparedEditorEffects struct {
 Mode *editorEffectApplyMode
 StyleVersion uuid.UUID
 FrozenRaw []byte
 Loaded v2draft.LoadedRenderRecipe
 Compilation v2draft.EditorEffectCompilation
}
func(s EditorEffectsService)prepareEditorEffects(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,req EditorEffectRequest,baseOrigin *authoredBaseApplyBinding,basePreparation bool)(preparedEditorEffects,error){
 invalid:=func()(preparedEditorEffects,error){return preparedEditorEffects{},domain.NewError("VALIDATION_FAILED",422)}
 catalog:=v2draft.SQLArtifactCatalog{DB:s.Drafts.DB,Bucket:s.Drafts.Bucket,ProjectID:pid,AssetVersionID:req.Source.AssetVersionID}
 obj,err:=catalog.Lookup(ctx,who,tid,v2draft.LayerRef{Ref:req.Recipe,Contract:"FrozenRenderRecipe"});if err!=nil{return preparedEditorEffects{},err}
 if obj.Bytes<2||obj.Bytes>1<<20||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"||obj.SHA256!=req.Recipe.SHA256{return invalid()}
 reader,err:=s.Drafts.Objects.Open(ctx,obj);if err!=nil{return preparedEditorEffects{},err};raw,readErr:=io.ReadAll(io.LimitReader(reader,obj.Bytes+1));closeErr:=reader.Close();if readErr!=nil{return preparedEditorEffects{},readErr};if closeErr!=nil{return preparedEditorEffects{},closeErr};sum:=sha256.Sum256(raw);if int64(len(raw))!=obj.Bytes||hex.EncodeToString(sum[:])!=req.Recipe.SHA256{return invalid()}
 frozen,err:=v2draft.DecodeFrozenRenderRecipe(raw);if err!=nil||frozen.Scalars.Source!=req.Source{return invalid()}
 loader:=v2draft.RenderRecipeSource{Resolver:v2draft.ClosureResolver{Catalog:catalog,Objects:s.Drafts.Objects},Learning:s.Drafts,ProjectID:pid}
 loaded,err:=loader.Load(ctx,who,tid,req.DraftDocumentID,frozen.Recipe,req.Source,frozen.Scalars.ICCLockSHA,frozen.Scalars.ModelLockSHA);if err!=nil{return preparedEditorEffects{},err}
 // Reconstructed scalar evidence must match the immutable submitted freeze;
 // do not adopt a newer session/learning correction during recovery.
 compiledFreeze,err:=v2draft.BuildFrozenRenderRecipeFromDocuments(loaded.Authored,loaded.Inputs,loaded.LayerBody,loaded.StyleBody,loaded.OverrideBodies,loaded.SourceSHA);if err!=nil{return invalid()};var recompiled v2draft.FrozenRenderRecipe;if json.Unmarshal(compiledFreeze,&recompiled)!=nil{return invalid()};a,_:=canonical.Marshal(recompiled.Scalars);b,_:=canonical.Marshal(frozen.Scalars);if !bytes.Equal(a,b){return invalid()}
 compilation,err:=v2draft.CompileEditorEffects(loaded.Authored,loaded.Inputs,loaded.StyleBody,loaded.OverrideBodies,loaded.SourceSHA);if err!=nil{return invalid()}
 closure,err:=loader.Resolver.Resolve(ctx,who,tid,loaded.Authored,req.Source);if err!=nil{return preparedEditorEffects{},err}
 compilation,err=loader.Resolver.BindEditorEffectProtection(ctx,who,tid,loaded.Authored,req.Source,loaded.SourceSHA,compilation);if err!=nil{return preparedEditorEffects{},err};digest,err:=canonical.Hash(compilation);if err!=nil{return invalid()}
 closurePins:=[]v2input.Ref{req.Recipe};seenClosure:=map[uuid.UUID]bool{req.Recipe.ArtifactID:true};for _,ref:=range closure{id,e:=uuid.Parse(ref.ArtifactID);if e!=nil{return invalid()};if !seenClosure[id]{seenClosure[id]=true;closurePins=append(closurePins,v2input.Ref{ArtifactID:id,SHA256:ref.SHA256})}}
 sourceParent,parentClosure,err:=s.sourceParent(ctx,who,tid,pid,req.Source);if err!=nil{return preparedEditorEffects{},err};for _,ref:=range parentClosure{if !seenClosure[ref.ArtifactID]{seenClosure[ref.ArtifactID]=true;closurePins=append(closurePins,ref)}};if len(closurePins)>4096{return invalid()}
 var precomposeProfile json.RawMessage
 if baseOrigin==nil&&!basePreparation{precomposeProfile,err=s.precomposeProfile(ctx,who,tid,pid,req,loader,loaded);if err!=nil{return preparedEditorEffects{},err}}
 executedStyle,err:=(v2draft.Publisher{DB:s.Drafts.DB,Objects:s.Drafts.Objects,Bucket:s.Drafts.Bucket}).PublishEditorExecutedStyle(ctx,who,tid,req.Source.AssetVersionID,uuid.NewSHA1(req.FrozenDocumentID,[]byte("editor-executed-style/"+digest)),compilation.Style);if err!=nil{return preparedEditorEffects{},err}
 mode:=&editorEffectApplyMode{ProjectID:pid,Binding:req.Source,FrozenRecipe:req.Recipe,FrozenDocumentID:req.FrozenDocumentID,DraftDocumentID:req.DraftDocumentID,DraftRevision:req.DraftRevision,SourceSHA:loaded.SourceSHA,CompilationSHA:digest,ExecutedStyle:executedStyle,SourceResult:sourceParent,Precompose:len(loaded.LayerBody)>0&&!basePreparation,PrecomposeProfile:precomposeProfile,Closure:closurePins,Override:compilation.Override,Intent:req,ItemID:uuid.NewSHA1(req.FrozenDocumentID,[]byte("editor-effect-source"))}
 if baseOrigin!=nil{
  copyOrigin:=*baseOrigin
  if copyOrigin.OriginalSource!=req.Source||copyOrigin.EffectiveStyle!=executedStyle{return invalid()}
  mode.AuthoredBase=&copyOrigin;mode.Precompose=false
 }
 var styleVersion uuid.UUID
 err=tenant.WithTenant(ctx,s.Runs.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if _,e:=project.Authorize(ctx,tx,tid,pid,who.UserID,role,domain.Editor,false);e!=nil{return e};if e:=mode.authorize(ctx,tx,who,tid,false);e!=nil{return e}
  return tx.QueryRow(ctx,`SELECT v.id FROM photo.style_versions v JOIN photo.styles st ON st.tenant_id=v.tenant_id AND st.id=v.style_id WHERE v.tenant_id=$1 AND st.project_id=$2 AND st.archived_at IS NULL AND v.export_artifact_id=$3 AND v.config_hash=$4 AND v.approval_status='accepted' ORDER BY v.id LIMIT 1`,tid,pid,frozen.Scalars.BaseStyle.ArtifactID,frozen.Scalars.BaseStyle.SHA256).Scan(&styleVersion)
 });if errors.Is(err,pgx.ErrNoRows){return preparedEditorEffects{},domain.NewError("STYLE_NOT_ACCEPTED",409)};if err!=nil{return preparedEditorEffects{},err}
 return preparedEditorEffects{Mode:mode,StyleVersion:styleVersion,FrozenRaw:raw,Loaded:loaded,Compilation:compilation},nil
}
