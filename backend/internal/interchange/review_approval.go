package interchange
import(
 "context"
 "encoding/json"
 "strconv"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// PrepareImportApproval publishes only the resolved scalar witness. The approved
// pointer still names the complete authored recipe, including masks and layers.
// No approval/pointer mutation happens until CommitDecisions accepts the batch.
func PrepareImportApproval(ctx context.Context,drafts v2draft.Service,who auth.Identity,tid,project uuid.UUID,target ImportReviewTarget)(ResolvedImportApproval,error){
 return prepareImportApproval(ctx,drafts,who,tid,project,target,nil,nil)
}

// Prepared reads are scoped to the same selected target and current review.
// This prepares a witness only; DecideImport must atomically register styles
// and approve the authored document before it becomes renderable.
func PrepareImportApprovalWithPreparedCatalog(ctx context.Context,drafts v2draft.Service,who auth.Identity,tid,project uuid.UUID,target ImportReviewTarget,prepared SQLImportApprovalCatalog)(ResolvedImportApproval,error){
 if err:=ctx.Err();err!=nil{return ResolvedImportApproval{},err}
 if prepared.DB!=drafts.DB||prepared.Scope.Tenant!=tid||prepared.Scope.Entry!=target.Entry||prepared.Who.UserID!=who.UserID||prepared.Who.SessionID!=who.SessionID{return ResolvedImportApproval{},v2input.ErrInvalid}
 return prepareImportApproval(ctx,drafts,who,tid,project,target,func(binding v2input.SessionVersionBinding,fallback v2draft.ArtifactCatalog)(v2draft.ArtifactCatalog,error){
  if prepared.Scope.Binding!=binding{return nil,v2input.ErrInvalid}
  return prepared.BuildApprovalCatalog(ctx,fallback)
 },nil)
}
// Review context is server-selected; the loaded draft supplies the exact binding.
func PrepareImportApprovalFromReview(ctx context.Context,drafts v2draft.Service,who auth.Identity,tid,project,workflow,run uuid.UUID,target ImportReviewTarget,revision int64,reportSHA string,parsed []byte,qualifier ImportStyleQualifier,factories ...ImportReviewStyleQualifierFactory)(ResolvedImportApproval,error){
 if err:=ctx.Err();err!=nil{return ResolvedImportApproval{},err}
 var factory ImportReviewStyleQualifierFactory
 if len(factories)>1{return ResolvedImportApproval{},v2input.ErrInvalid};if len(factories)==1{factory=factories[0]}
 if qualifier==nil&&factory==nil{return ResolvedImportApproval{},domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 if qualifier!=nil&&factory!=nil{return ResolvedImportApproval{},v2input.ErrInvalid}
 owned:=append([]byte(nil),parsed...)
 return prepareImportApproval(ctx,drafts,who,tid,project,target,func(binding v2input.SessionVersionBinding,fallback v2draft.ArtifactCatalog)(v2draft.ArtifactCatalog,error){
  prepared:=SQLImportApprovalCatalog{DB:drafts.DB,Who:who,Revision:revision,ReportSHA:reportSHA,Parsed:owned,Bucket:drafts.Bucket,
   Scope:ImportPreparedScope{Tenant:tid,Run:run,Workflow:workflow,Entry:target.Entry,Binding:binding}}
  return prepared.BuildApprovalCatalog(ctx,fallback)
 },func(binding v2input.SessionVersionBinding,style v2input.Ref,body,recipe []byte)(*ImportStyleApprovalReceipt,error){
  scope:=ImportPreparedScope{Tenant:tid,Run:run,Workflow:workflow,Entry:target.Entry,Binding:binding}
  installed:=qualifier
  if factory!=nil{
   prepared:=SQLImportApprovalCatalog{DB:drafts.DB,Scope:scope,Who:who,Revision:revision,ReportSHA:reportSHA,Parsed:owned,Bucket:drafts.Bucket}
   var err error;installed,err=factory.BuildReviewStyleQualifier(ctx,prepared,project,drafts.Objects);if err!=nil{return nil,err}
   if installed==nil{return nil,v2input.ErrInvalid}
  }
  registration,err:=PrepareImportStyleRegistration(ctx,scope,owned,style,body,installed);if err!=nil{return nil,err}
  receipt:=&ImportStyleApprovalReceipt{Parsed:string(owned),Recipe:string(recipe),Binding:binding,Style:registration.Config,Config:string(registration.Body)}
  if err:=validateImportStyleApprovalReceipt(workflow,target.Entry,*receipt);err!=nil{return nil,err}
  return receipt,nil
 })
}
func prepareImportApproval(ctx context.Context,drafts v2draft.Service,who auth.Identity,tid,project uuid.UUID,target ImportReviewTarget,catalogFactory func(v2input.SessionVersionBinding,v2draft.ArtifactCatalog)(v2draft.ArtifactCatalog,error),stylePreparation func(v2input.SessionVersionBinding,v2input.Ref,[]byte,[]byte)(*ImportStyleApprovalReceipt,error))(ResolvedImportApproval,error){
 if err:=ctx.Err();err!=nil{return ResolvedImportApproval{},err}
 invalid:=func()(ResolvedImportApproval,error){return ResolvedImportApproval{},domain.NewError("INTERCHANGE_DRAFT_CHANGED",409)}
 if drafts.DB==nil||drafts.Objects==nil||target.State!="proposed"||target.Version==nil||target.Document==nil||target.Stream==nil||target.Recipe==nil||project==uuid.Nil{return invalid()}
 var asset uuid.UUID;var revision int64
 err:=tenant.WithTenant(ctx,drafts.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,`SELECT a.id,s.revision FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
  JOIN photo.v2_document_streams s ON s.tenant_id=v.tenant_id AND s.asset_version_id=v.id AND s.project_id=a.project_id
  WHERE v.tenant_id=$1 AND v.id=$2 AND a.project_id=$3 AND s.id=$4 AND s.current_document_id=$5 AND s.approved_document_id IS NULL`,tid,*target.Version,project,*target.Stream,*target.Document).Scan(&asset,&revision)
 });if err!=nil{return ResolvedImportApproval{},err}
 draft,err:=drafts.ReadApprovalDraft(ctx,who,tid,asset,*target.Document,revision);if err!=nil{return ResolvedImportApproval{},err}
 if draft.ProjectID!=project||draft.AssetVersionID!=*target.Version||draft.StreamID!=*target.Stream||draft.DocumentID!=*target.Document||draft.Approved||rawSHA(draft.Recipe)!=target.Recipe.SHA256{return invalid()}
 var fields struct{Learning *uuid.UUID `json:"learning_version_id"`;Session *v2input.Ref `json:"session_corrections"`}
 if json.Unmarshal(draft.Recipe,&fields)!=nil||fields.Learning!=nil{return invalid()}
 var modelLock string
 err=tenant.WithTenant(ctx,drafts.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  if fields.Session==nil{return tx.QueryRow(ctx,`SELECT model_lock_sha FROM photo.resolve_v2_qualification($1,$2,'session.describe','1')`,tid,project).Scan(&modelLock)}
  return tx.QueryRow(ctx,`SELECT sa.model_lock_sha FROM photo.session_analyses sa JOIN photo.artifacts a ON a.tenant_id=sa.tenant_id AND a.id=sa.plan_artifact_id
   WHERE sa.tenant_id=$1 AND sa.project_id=$2 AND sa.plan_artifact_id=$3 AND a.sha256=$4 AND a.state='ready' AND a.verified_at IS NOT NULL`,tid,project,fields.Session.ArtifactID,fields.Session.SHA256).Scan(&modelLock)
 });if err!=nil{return ResolvedImportApproval{},err}
 var catalog v2draft.ArtifactCatalog=v2draft.SQLArtifactCatalog{DB:drafts.DB,Bucket:drafts.Bucket,ProjectID:project,AssetVersionID:draft.AssetVersionID}
 if catalogFactory!=nil{catalog,err=catalogFactory(draft.Binding,catalog);if err!=nil{return ResolvedImportApproval{},err};if catalog==nil{return invalid()}}
 resolver:=v2draft.ClosureResolver{Catalog:catalog,Objects:drafts.Objects}
 loaded,err:=(v2draft.RenderRecipeSource{Resolver:resolver,ProjectID:project}).Load(ctx,who,tid,draft.DocumentID,draft.Recipe,draft.Binding,draft.Image.Color.ICCSHA256,modelLock);if err!=nil{return ResolvedImportApproval{},err}
 var styleReceipt *ImportStyleApprovalReceipt
 if stylePreparation!=nil{styleReceipt,err=stylePreparation(draft.Binding,loaded.Inputs.BaseStyle,loaded.StyleBody,draft.Recipe);if err!=nil{return ResolvedImportApproval{},err}}
 if styleReceipt!=nil&&(styleReceipt.Recipe!=string(draft.Recipe)||rawSHA([]byte(styleReceipt.Recipe))!=target.Recipe.SHA256){return invalid()}

 // Prove complete layers/ordered overrides before extracting the scalar witness;
 // the full authored recipe remains the immutable document approved by SQL.
 full,err:=v2draft.BuildFrozenRenderRecipeFromDocuments(loaded.Authored,loaded.Inputs,loaded.LayerBody,loaded.StyleBody,loaded.OverrideBodies,loaded.SourceSHA);if err!=nil{return ResolvedImportApproval{},err}
 var compiled v2draft.FrozenRenderRecipe
 if json.Unmarshal(full,&compiled)!=nil{return invalid()}
 frozen,err:=canonical.Marshal(compiled.Scalars);if err!=nil{return ResolvedImportApproval{},err}
 id:=uuid.NewSHA1(uuid.NameSpaceOID,[]byte("v2-frozen/"+tid.String()+"/"+draft.DocumentID.String()))
 ref,err:=(v2draft.Publisher{DB:drafts.DB,Objects:drafts.Objects,Bucket:drafts.Bucket}).Publish(ctx,who,tid,draft.AssetVersionID,id,frozen);if err!=nil{return ResolvedImportApproval{},err}
 if err:=ctx.Err();err!=nil{return ResolvedImportApproval{},err}
 return ResolvedImportApproval{Entry:target.Entry,StreamRevision:strconv.FormatInt(revision,10),Artifact:ref.ArtifactID,SHA:ref.SHA256,StyleRegistration:styleReceipt},nil
}
