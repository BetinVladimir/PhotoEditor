package v2draft

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "strconv"
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

// A missing receipt must roll back the provisional Use row. An accepted reply
// is proven against its original immutable document, not the latest draft head.
func(s Service)replayApproval(ctx context.Context,who auth.Identity,tid,asset uuid.UUID,key string,request ApprovalRequest)(idempotency.Response,bool,error){
 var out idempotency.Response
 if s.DB==nil{return out,false,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 missing:=errors.New("DRAFT_APPROVAL_NOT_ACCEPTED")
 var version,stream,projectID uuid.UUID
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  e:=tx.QueryRow(ctx,`SELECT d.project_id,s.id,s.asset_version_id FROM photo.v2_documents d JOIN photo.v2_document_streams s ON s.tenant_id=d.tenant_id AND s.project_id=d.project_id AND s.id=d.stream_id JOIN photo.asset_versions v ON v.tenant_id=s.tenant_id AND v.id=s.asset_version_id JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE d.tenant_id=$1 AND d.id=$2 AND d.document_type='EditingRecipeV3' AND s.stream_type='draft' AND a.id=$3 AND a.deleted_at IS NULL AND a.upload_state='verified' AND v.review_state='accepted'`,tid,request.DocumentID,asset).Scan(&projectID,&stream,&version)
  // The fresh pipeline owns missing documents and its original error semantics.
  if errors.Is(e,pgx.ErrNoRows){return missing};if e!=nil{return e}
  if _,e=project.Authorize(ctx,tx,tid,projectID,who.UserID,role,domain.Editor,false);e!=nil{return e}
  out,e=idempotency.Use(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Draft.Approve."+asset.String(),Key:key},request,time.Now().Add(24*time.Hour),func()(idempotency.Response,error){return idempotency.Response{},missing})
  if e!=nil{return e}
  if out.Status!=200||out.ResourceID==nil||*out.ResourceID!=stream{return domain.NewError("DOCUMENT_INVALID",422)}
  approval,revision,e:=decodeApprovalReceipt(out.Body,stream,request.DocumentID);if e!=nil{return e}
  expected,e:=replaceRevision(request.ExpectedRevision);if e!=nil||revision<expected||revision-expected>1{return domain.NewError("DOCUMENT_INVALID",422)}
  var live bool
  e=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.v2_document_approvals approved JOIN photo.v2_documents d ON d.tenant_id=approved.tenant_id AND d.id=approved.document_id AND d.project_id=approved.project_id AND d.sha256=approved.recipe_sha JOIN photo.artifacts resolved ON resolved.tenant_id=approved.tenant_id AND resolved.id=approved.resolved_artifact_id WHERE approved.tenant_id=$1 AND approved.project_id=$2 AND approved.id=$3 AND approved.document_id=$4 AND resolved.state='ready' AND resolved.verified_at IS NOT NULL AND resolved.object_version IS NOT NULL AND resolved.object_version NOT IN('','null') AND (resolved.expires_at IS NULL OR resolved.expires_at>clock_timestamp()) AND EXISTS(SELECT FROM photo.v26_region_source($1,$6,$7,$5,$8) source WHERE source.project_id=$2))`,tid,projectID,approval,request.DocumentID,who.UserID,asset,version,who.SessionID).Scan(&live)
  if e!=nil{return e};if !live{return domain.NewError("DOCUMENT_INVALID",422)}
  return idempotency.ValidateReceiptAuthority(ctx,tx)
 })
 if errors.Is(err,missing){return idempotency.Response{},false,nil};if err!=nil{return idempotency.Response{},false,draftMutationError(err)}
 // This existing generic reader verifies canonical original bytes and repeats
 // current learning-consent and post-object catalogue/project authority.
 body,err:=s.GetDocument(ctx,who,tid,request.DocumentID);if err!=nil{return idempotency.Response{},false,err}
 var recipe struct{Source v2input.SessionVersionBinding `json:"source"`}
 if json.Unmarshal(body,&recipe)!=nil||recipe.Source.AssetVersionID!=version||v2input.ValidateEditingRecipeV3(body,recipe.Source)!=nil{return idempotency.Response{},false,domain.NewError("DOCUMENT_INVALID",422)}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var live bool
  if e:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.v26_region_source($1,$2,$3,$4,$5) source WHERE source.project_id=$6 AND source.artifact_id=$7 AND source.sha256=$8)`,tid,asset,version,who.UserID,who.SessionID,projectID,recipe.Source.Source.ArtifactID,recipe.Source.Source.SHA256).Scan(&live);e!=nil{return e};if !live{return domain.NewError("DOCUMENT_INVALID",422)}
  role,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e};_,e=project.Authorize(ctx,tx,tid,projectID,who.UserID,role,domain.Editor,false);return e
 });if err!=nil{return idempotency.Response{},false,err}
 if err=ctx.Err();err!=nil{return idempotency.Response{},false,err}
 return out,true,nil
}
func decodeApprovalReceipt(raw []byte,stream,document uuid.UUID)(uuid.UUID,int64,error){
 invalid:=func()(uuid.UUID,int64,error){return uuid.Nil,0,domain.NewError("DOCUMENT_INVALID",422)}
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil||len(fields)!=4{return invalid()}
 for _,key:=range []string{"id","revision","document_id","approval_id"}{if _,ok:=fields[key];!ok{return invalid()}}
 var body struct{ID uuid.UUID `json:"id"`;Revision string `json:"revision"`;Document uuid.UUID `json:"document_id"`;Approval uuid.UUID `json:"approval_id"`}
 dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields();if dec.Decode(&body)!=nil||dec.Decode(new(any))!=io.EOF||body.ID!=stream||body.Document!=document||body.Approval==uuid.Nil{return invalid()}
 revision,err:=strconv.ParseInt(body.Revision,10,64);if err!=nil||revision<1||strconv.FormatInt(revision,10)!=body.Revision{return invalid()}
 return body.Approval,revision,nil
}
