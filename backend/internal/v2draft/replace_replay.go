package v2draft

import (
 "context"
 "encoding/json"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// Probe the original immutable command before resolving today's draft head or
// publishing anything. A missing receipt rolls back its provisional Use row.
func(s Service) replayReplacement(ctx context.Context,who auth.Identity,tid,asset uuid.UUID,key string,request ReplaceRequest)(idempotency.Response,bool,error){
 missing:=errors.New("DRAFT_REPLACEMENT_NOT_ACCEPTED")
 var out idempotency.Response
 var projectID,version,document uuid.UUID
 var pinned v2input.Ref
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var e error
  out,e=idempotency.Use(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Draft.Replace."+asset.String(),Key:key},request,time.Now().Add(24*time.Hour),func()(idempotency.Response,error){return idempotency.Response{},missing})
  if e!=nil{return e}
  var body struct{ID uuid.UUID `json:"id"`;Revision string `json:"revision"`;Document v2input.Ref `json:"document"`;Data struct{ID uuid.UUID `json:"id"`;Tenant uuid.UUID `json:"tenant_id"`;Project uuid.UUID `json:"project_id"`;Version uuid.UUID `json:"asset_version_id"`;Current uuid.UUID `json:"current_document_id"`;Revision string `json:"revision"`;Kind string `json:"resource_type"`;Stream string `json:"stream_type"`} `json:"data"`}
  expected,e:=replaceRevision(request.ExpectedRevision);if e!=nil{return e}
  if json.Unmarshal(out.Body,&body)!=nil||out.Status!=200||out.ResourceID==nil||*out.ResourceID!=body.ID||body.ID==uuid.Nil||body.Data.ID!=body.ID||body.Data.Tenant!=tid||body.Data.Project==uuid.Nil||body.Data.Version==uuid.Nil||body.Data.Kind!="v2_document_streams"||body.Data.Stream!="draft"||body.Data.Revision!=body.Revision{return domain.NewError("DOCUMENT_INVALID",422)}
  revision,e:=replaceRevision(body.Revision);if e!=nil||(expected==0&&revision!=2)||(expected>0&&(revision<=expected||revision-expected!=1)){return domain.NewError("DOCUMENT_INVALID",422)}
  document=stableDraftID(tid,who.UserID,asset,key,"recipe")
  if body.Data.Current!=document||body.Document.ArtifactID==uuid.Nil||len(body.Document.SHA256)!=64{return domain.NewError("DOCUMENT_INVALID",422)}
  projectID,version,pinned=body.Data.Project,body.Data.Version,body.Document
  var live bool
  e=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.v2_documents d JOIN photo.v2_document_streams s ON s.tenant_id=d.tenant_id AND s.id=d.stream_id JOIN photo.asset_versions v ON v.tenant_id=s.tenant_id AND v.id=s.asset_version_id JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE d.tenant_id=$1 AND d.project_id=$2 AND d.id=$3 AND d.document_type='EditingRecipeV3' AND d.artifact_id=$4 AND d.sha256=$5 AND s.id=$6 AND s.project_id=$2 AND s.asset_version_id=$7 AND s.stream_type='draft' AND a.id=$8 AND a.deleted_at IS NULL AND a.upload_state='verified' AND v.review_state='accepted')`,tid,projectID,document,pinned.ArtifactID,pinned.SHA256,body.ID,version,asset).Scan(&live)
  if e!=nil{return e};if !live{return domain.NewError("DOCUMENT_INVALID",422)}
  role,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e};if _,e=project.Authorize(ctx,tx,tid,projectID,who.UserID,role,domain.Editor,false);e!=nil{return e}
  return idempotency.ValidateReceiptAuthority(ctx,tx)
 })
 if errors.Is(err,missing){return idempotency.Response{},false,nil};if err!=nil{return idempotency.Response{},false,draftMutationError(err)}
 body,err:=s.GetDocument(ctx,who,tid,document);if err!=nil{return idempotency.Response{},false,err}
 var recipe struct{Source v2input.SessionVersionBinding `json:"source"`}
 if json.Unmarshal(body,&recipe)!=nil||recipe.Source.AssetVersionID!=version||v2input.ValidateEditingRecipeV3(body,recipe.Source)!=nil{return idempotency.Response{},false,domain.NewError("DOCUMENT_INVALID",422)}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var live bool
  if e:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.v26_region_source($1,$2,$3,$4,$5) source WHERE source.project_id=$6 AND source.artifact_id=$7 AND source.sha256=$8)`,tid,asset,version,who.UserID,who.SessionID,projectID,recipe.Source.Source.ArtifactID,recipe.Source.Source.SHA256).Scan(&live);e!=nil{return e};if !live{return domain.NewError("DOCUMENT_INVALID",422)}
  role,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e};_,e=project.Authorize(ctx,tx,tid,projectID,who.UserID,role,domain.Editor,false);return e
 });if err!=nil{return idempotency.Response{},false,err};if err=ctx.Err();err!=nil{return idempotency.Response{},false,err};return out,true,nil
}
