package semanticreview

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// The shown task copies the approved pixels without rerendering. Its exact
// canonical preview PNG is therefore the display of this target, never a run's
// first output (a child run may contain many photographs).
type ShownAppliedPreview struct {
 SchemaVersion int `json:"schema_version"`
 TenantID uuid.UUID `json:"tenant_id"`
 TargetID uuid.UUID `json:"target_id"`
 ChildRunID uuid.UUID `json:"child_run_id"`
 WorkingImage v2input.Ref `json:"working_image"`
 After PreviewMedia `json:"after"`
}
type shownPreviewSigner interface { SignRead(context.Context,objectstore.Object,time.Duration)(objectstore.ReadURL,error) }
type ShownAppliedPreviewService struct { db artifactjson.RowQuerier; store planObjectStore; signer shownPreviewSigner; bucket,srgbSHA string }
func NewShownAppliedPreviewService(db artifactjson.RowQuerier,store planObjectStore,signer shownPreviewSigner,bucket,srgbSHA string)(*ShownAppliedPreviewService,error){
 if db==nil||store==nil||signer==nil||bucket==""||!shaPattern.MatchString(srgbSHA){return nil,ErrInvalid}
 return &ShownAppliedPreviewService{db:db,store:store,signer:signer,bucket:bucket,srgbSHA:srgbSHA},nil
}
type appliedPresentation struct {
 SchemaVersion int `json:"schema_version"`
 Params json.RawMessage `json:"params"`
 NumericalEvidence v2input.Ref `json:"numerical_evidence"`
 WorkingImage v2input.Ref `json:"working_image"`
 Pixels v2input.Ref `json:"pixels"`
 Before v2input.Ref `json:"before"`
 After v2input.Ref `json:"after"`
 Affected v2input.Ref `json:"affected"`
 Protected v2input.Ref `json:"protected"`
 Width int `json:"width"`
 Height int `json:"height"`
 ICCLockSHA string `json:"icc_lock_sha"`
 SRGBSHA string `json:"srgb_sha"`
 Resize string `json:"resize"`
 Overlay string `json:"overlay"`
}
func decodeAppliedPresentation(raw []byte,p ShownExposureAdoptionParams,srgb string)(appliedPresentation,error){
 var a appliedPresentation
 if decodeStrict(raw,&a)!=nil||a.SchemaVersion!=1||a.Width<1||a.Height<1||a.Width>2560||a.Height>2560||a.SRGBSHA!=srgb||!shaPattern.MatchString(a.ICCLockSHA)||a.Resize!="area_linear_premult_v1"||a.Overlay!="max_coverage_ceil_alpha_v1"||a.WorkingImage!=p.WorkingImage||a.Pixels!=p.Pixels||a.NumericalEvidence!=p.NumericalEvidence {return a,ErrInvalid}
 for _,ref:=range []v2input.Ref{a.Before,a.After,a.Affected,a.Protected}{if ref.ArtifactID==uuid.Nil||!shaPattern.MatchString(ref.SHA256){return a,ErrInvalid}}
 var identity struct {TenantID uuid.UUID `json:"tenant_id"`;TargetID uuid.UUID `json:"target_id"`}
 if json.Unmarshal(a.Params,&identity)!=nil||identity.TenantID!=p.TenantID||identity.TargetID!=p.TargetID{return a,ErrInvalid}
 return a,nil
}
func decodeAppliedParams(raw []byte,tenant,target uuid.UUID)(ShownExposureAdoptionParams,error){
 var p ShownExposureAdoptionParams
 if decodeStrict(raw,&p)!=nil||p.TenantID!=tenant||p.TargetID!=target{return p,ErrInvalid}
 // All four byte-copy contracts have identical fields, but operation-specific
 // producer UUID validation must remain exact.
 var err error
 switch p.PreviewTaskID {
 case uuid.NewSHA1(p.PreviewRunID,[]byte("semantic.preview_local_exposure:"+target.String())): _,_,err=FreezeShownExposureAdoptionParams(p)
 case uuid.NewSHA1(p.PreviewRunID,[]byte("semantic.preview_remove_object:"+target.String())): _,_,err=FreezeShownRemovalAdoptionParams(ShownRemovalAdoptionParams(p))
 case uuid.NewSHA1(p.PreviewRunID,[]byte("semantic.preview_background_cleanup:"+target.String())): _,_,err=FreezeShownBackgroundCleanupAdoptionParams(ShownBackgroundCleanupAdoptionParams(p))
 case uuid.NewSHA1(p.PreviewRunID,[]byte("semantic.preview_retouch_region:"+target.String())): _,_,err=FreezeShownRetouchAdoptionParams(ShownRetouchAdoptionParams(p))
 default: err=ErrInvalid
 }
 return p,err
}
func (s *ShownAppliedPreviewService) ReadAppliedPreview(ctx context.Context,who auth.Identity,tenant,target uuid.UUID)(ShownAppliedPreview,error){
 empty:=ShownAppliedPreview{}
 if err:=ctx.Err();err!=nil{return empty,err}
 if s==nil||s.db==nil||s.store==nil||s.signer==nil{return empty,ErrUnavailable}
 if tenant==uuid.Nil||target==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return empty,ErrInvalid}
 read:=func(ctx context.Context,source PlanSource)([]byte,error){
  if source.Bucket!=s.bucket||source.Key==""||source.Version==""||source.Version=="null"||source.Bytes<2||source.Bytes>33882112||!shaPattern.MatchString(source.Ref.SHA256){return nil,ErrInvalid}
  if source.MediaType!="application/json"&&source.MediaType!="image/png"{return nil,ErrInvalid}
  if err:=ctx.Err();err!=nil{return nil,err}
  stream,err:=s.store.Open(ctx,objectstore.Object{Key:source.Key,VersionID:source.Version,SHA256:source.Ref.SHA256,Bytes:source.Bytes,ContentType:source.MediaType});if err!=nil{return nil,err};if stream==nil{return nil,ErrInvalid}
  raw,err:=io.ReadAll(io.LimitReader(stream,source.Bytes+1));closed:=stream.Close();if err!=nil{return nil,err};if closed!=nil{return nil,closed};if err=ctx.Err();err!=nil{return nil,err}
  sum:=sha256.Sum256(raw);if int64(len(raw))!=source.Bytes||hex.EncodeToString(sum[:])!=source.Ref.SHA256{return nil,ErrInvalid};return raw,nil
 }
 load:=func(kind string,params,presentation []byte)(PlanSource,error){
  var a PlanSource
  err:=s.db.QueryRow(ctx,"SELECT contract,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.semantic_shown_applied_preview_object($1,$2,$3,$4,$5,$6,$7)",tenant,target,who.UserID,who.SessionID,kind,string(params),string(presentation)).Scan(&a.Contract,&a.Ref.ArtifactID,&a.Ref.SHA256,&a.Bytes,&a.Bucket,&a.Key,&a.Version,&a.MediaType)
  if errors.Is(err,pgx.ErrNoRows){return a,ErrConflict};if err!=nil{return a,err};if a.Ref.ArtifactID==uuid.Nil{return a,ErrInvalid};return a,ctx.Err()
 }
 paramsSource,err:=load("params",nil,nil);if err!=nil{return empty,err}
 paramsRaw,err:=read(ctx,paramsSource);if err!=nil{return empty,err}
 p,err:=decodeAppliedParams(paramsRaw,tenant,target);if err!=nil{return empty,err}
 presentationSource,err:=load("presentation",paramsRaw,nil);if err!=nil{return empty,err}
 if presentationSource.Ref!=p.Presentation||presentationSource.MediaType!="application/json"{return empty,ErrInvalid}
 presentationRaw,err:=read(ctx,presentationSource);if err!=nil{return empty,err}
 a,err:=decodeAppliedPresentation(presentationRaw,p,s.srgbSHA);if err!=nil{return empty,err}
 source,err:=load("source_image",paramsRaw,presentationRaw);if err!=nil{return empty,err}
 if source.Ref!=p.WorkingImage||source.MediaType!="application/json"{return empty,ErrInvalid}
 originalRaw,err:=read(ctx,source);if err!=nil{return empty,err}
 original,err:=review.ParseWorkingImage(originalRaw);if err!=nil{return empty,ErrInvalid}
 shown,err:=ReadShownAppliedDocument(ctx,s.db,who,tenant,target,s.bucket,read);if err!=nil{return empty,err}
 copied,err:=review.ParseWorkingImage(shown.Body);if err!=nil{return empty,ErrInvalid}
 if original.Pixels.ID!=p.Pixels.ArtifactID||original.Pixels.SHA256!=p.Pixels.SHA256||copied.Pixels.SHA256!=original.Pixels.SHA256{return empty,ErrInvalid}
 copied.Pixels=original.Pixels
 left,_:=canonical.Marshal(copied);right,_:=canonical.Marshal(original);if string(left)!=string(right){return empty,ErrInvalid}
 media,err:=load("after",paramsRaw,presentationRaw);if err!=nil{return empty,err}
 if media.Ref!=a.After||media.MediaType!="image/png"{return empty,ErrInvalid}
 png,err:=read(ctx,media);if err!=nil{return empty,err}
 if err=VerifyExposurePreviewPNG(ctx,png,media.Ref.SHA256,a.Width,a.Height,s.srgbSHA,"after");err!=nil{return empty,err}
 check:=func()error{
  for _,item:=range []struct{kind string;before PlanSource}{{"params",paramsSource},{"presentation",presentationSource},{"source_image",source},{"after",media}}{current,e:=load(item.kind,paramsRaw,presentationRaw);if e!=nil{return e};if current!=item.before{return ErrConflict}}
  proof,e:=LoadShownAppliedImage(ctx,s.db,who,tenant,target);if e!=nil{return e};if proof.ChildRunID!=p.ChildRunID||proof.WorkingImage!=shown.Source.Ref{return ErrConflict};return ctx.Err()
 }
 if err=check();err!=nil{return empty,err}
 signed,err:=s.signer.SignRead(ctx,objectstore.Object{Key:media.Key,VersionID:media.Version,SHA256:media.Ref.SHA256,Bytes:media.Bytes,ContentType:media.MediaType},time.Minute);if err!=nil{return empty,err}
 if !previewURL(signed.URL)||!signed.ExpiresAt.After(time.Now())||signed.ExpiresAt.After(time.Now().Add(61*time.Second)){return empty,ErrInvalid}
 if err=check();err!=nil{return empty,err}
 return ShownAppliedPreview{SchemaVersion:1,TenantID:tenant,TargetID:target,ChildRunID:p.ChildRunID,WorkingImage:shown.Source.Ref,After:PreviewMedia{ArtifactID:media.Ref.ArtifactID,SHA256:media.Ref.SHA256,URL:signed.URL,ExpiresAt:signed.ExpiresAt,Width:a.Width,Height:a.Height,MediaType:"image/png"}},nil
}
