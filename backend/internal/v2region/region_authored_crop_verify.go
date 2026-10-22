package v2region

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type authoredCropObjectRow struct {
 Canonical AuthoredCropVerification
 Root,Derivative objectstore.Object
 DerivativeRef v2input.Ref
 Manifest string
}
type authoredCropManifest struct {
 Schema int `json:"schema_version"`;IntentSHA string `json:"intent_sha256"`;ParamsSHA string `json:"params_sha256"`;Request uuid.UUID `json:"request_id"`
 Source v2input.Ref `json:"source"`;Recipe v2input.Ref `json:"frozen_recipe"`;Render v2input.Ref `json:"render_result"`;Image v2input.Ref `json:"rendered_image"`
 Canvas review.Canvas `json:"target_canvas"`;Chain json.RawMessage `json:"target_transform_chain"`;Geometry RegionGeometry `json:"geometry"`
 Derivative v2input.Ref `json:"derivative"`;Bytes int64 `json:"bytes"`;ICCSHA string `json:"icc_sha256"`;Media string `json:"media_type"`
 Alpha bool `json:"alpha"`;Review bool `json:"review_required"`;Metadata string `json:"metadata_policy"`;Quality int `json:"quality"`
}
// SQLAuthoredCropVerifier consumes the independently replayed specialized
// adoption receipt, exact physical versions and full render documents. It does
// not treat a generic inbox row, completed run or submitted manifest as proof.
type SQLAuthoredCropVerifier struct { Evidence SQLAuthoredRenderEvidence }
func(s SQLAuthoredCropVerifier)row(ctx context.Context,r AuthoredRegionRecord)(authoredCropObjectRow,error){
 var out authoredCropObjectRow;var rootBucket,derivativeBucket string
 if s.Evidence.API==nil||s.Evidence.Bucket==""{return out,ErrRegionUnavailable}
 if err:=requireAuthoredPoolRole(ctx,s.Evidence.API,"photo_api");err!=nil{return out,err}
 err:=database.TenantTransaction(ctx,s.Evidence.API,r.Intent.Tenant,func(tx pgx.Tx)error{
  return tx.QueryRow(ctx,"SELECT * FROM photo.authored_region_crop_media($1,$2,$3,$4)",r.Intent.Tenant,r.ID,r.Intent.Actor,r.Intent.Session).Scan(
   &out.Canonical.Run,&out.Canonical.Task,&out.Canonical.Result,&out.Canonical.Attempt,&out.Canonical.Fence,
   &out.Canonical.Root,&out.Canonical.RootSHA,&rootBucket,&out.Root.Key,&out.Root.VersionID,&out.Root.Bytes,
   &out.Manifest,&out.DerivativeRef.ArtifactID,&out.DerivativeRef.SHA256,&derivativeBucket,&out.Derivative.Key,&out.Derivative.VersionID,&out.Derivative.Bytes,&out.Derivative.ContentType)
 });if err!=nil{return out,err}
 out.Canonical.Request=r.ID;out.Canonical.IntentSHA=r.SHA;out.Root.SHA256=out.Canonical.RootSHA;out.Root.ContentType="application/json";out.Derivative.SHA256=out.DerivativeRef.SHA256
 workflow,_:=v2input.WorkflowID(r.CropRun);task,_:=v2input.TaskID(workflow,"region.authored_crop",0)
 if rootBucket!=s.Evidence.Bucket||derivativeBucket!=s.Evidence.Bucket||out.Canonical.Run!=r.CropRun||out.Canonical.Task!=task||out.Canonical.Result==uuid.Nil||out.Canonical.Attempt<1||out.Canonical.Attempt>6||out.Canonical.Fence<1||out.Canonical.Root==uuid.Nil||!validRegionSHA(out.Canonical.RootSHA)||out.DerivativeRef.ArtifactID==uuid.Nil||!validRegionSHA(out.DerivativeRef.SHA256){return authoredCropObjectRow{},ErrRegionInvalid}
 for _,object:=range []objectstore.Object{out.Root,out.Derivative}{if object.Key==""||object.VersionID==""||object.VersionID=="null"||object.Bytes<2{return authoredCropObjectRow{},ErrRegionInvalid}}
 if out.Root.Bytes>1048576||out.Derivative.Bytes>16777216||(out.Derivative.ContentType!="image/jpeg"&&out.Derivative.ContentType!="image/webp"){return authoredCropObjectRow{},ErrRegionInvalid}
 return out,nil
}
func parseAuthoredCropManifest(raw []byte,r AuthoredRegionRecord,plan AuthoredCropPlan,row authoredCropObjectRow)(authoredCropManifest,error){
 var out authoredCropManifest
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return out,ErrRegionInvalid}
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return out,ErrRegionInvalid}
 keys:=[]string{"schema_version","intent_sha256","params_sha256","request_id","source","frozen_recipe","render_result","rendered_image","target_canvas","target_transform_chain","geometry","derivative","bytes","icc_sha256","media_type","alpha","review_required","metadata_policy","quality"}
 if len(fields)!=len(keys){return out,ErrRegionInvalid};for _,key:=range keys{if _,ok:=fields[key];!ok{return out,ErrRegionInvalid}}
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields();if decoder.Decode(&out)!=nil{return out,ErrRegionInvalid}
 left,e:=canonical.Bytes(out.Chain);right,f:=canonical.Bytes(plan.rendered.TransformChain)
 if out.Schema!=1||out.IntentSHA!=r.SHA||out.ParamsSHA!=plan.sha||out.Request!=r.ID||out.Source!=r.Intent.Binding.Source||out.Recipe!=r.Intent.Recipe||out.Render!=plan.params.RenderResult||out.Image!=plan.params.RenderedImage||out.Canvas!=plan.rendered.Canvas||e!=nil||f!=nil||!bytes.Equal(left,right)||out.Derivative!=row.DerivativeRef||out.Bytes!=row.Derivative.Bytes||out.ICCSHA!=r.Intent.TargetICCSHA||out.Media!=row.Derivative.ContentType||out.Alpha!=(plan.rendered.Alpha!=nil)||out.Review!=plan.reviewRequired||out.Metadata!="stripped"||out.Quality!=88{return authoredCropManifest{},ErrRegionInvalid}
 g:=out.Geometry
 if g.Left<0||g.Top<0||g.Width<1||g.Height<1||g.Width>out.Canvas.Width||g.Height>out.Canvas.Height||g.Left>out.Canvas.Width-g.Width||g.Top>out.Canvas.Height-g.Height||g.OutputWidth<1||g.OutputHeight<1||g.OutputWidth>int64(r.Intent.MaxEdge)||g.OutputHeight>int64(r.Intent.MaxEdge)||g.OutputWidth>g.Width||g.OutputHeight>g.Height{return authoredCropManifest{},ErrRegionInvalid}
 if out.Media!="image/jpeg"&&out.Media!="image/webp"||out.Alpha!=(out.Media=="image/webp"){return authoredCropManifest{},ErrRegionInvalid}
 return out,nil
}
func readAuthoredDerivative(ctx context.Context,objects RegionObjectReader,object objectstore.Object,ref v2input.Ref)([]byte,error){
 if objects==nil||object.Bytes<2||object.Bytes>16777216||object.SHA256!=ref.SHA256||object.Key==""||object.VersionID==""||object.VersionID=="null"||(object.ContentType!="image/jpeg"&&object.ContentType!="image/webp"){return nil,ErrRegionInvalid}
 if err:=ctx.Err();err!=nil{return nil,err}
 stream,err:=objects.Open(ctx,object)
 if err!=nil{if stream!=nil{_ = stream.Close()};return nil,err};if stream==nil{return nil,ErrRegionInvalid}
 body,readErr:=io.ReadAll(io.LimitReader(checkedRegionReader{ctx,stream},object.Bytes+1));closeErr:=stream.Close()
 if readErr!=nil{return nil,readErr};if closeErr!=nil{return nil,closeErr};if err=ctx.Err();err!=nil{return nil,err}
 digest:=sha256.Sum256(body);if int64(len(body))!=object.Bytes||hex.EncodeToString(digest[:])!=ref.SHA256{return nil,ErrRegionInvalid};return body,nil
}
func(s SQLAuthoredCropVerifier)load(ctx context.Context,r AuthoredRegionRecord)(authoredCropObjectRow,authoredCropManifest,[]byte,[]byte,error){
 var empty authoredCropObjectRow;var noManifest authoredCropManifest
 if r.validate()!=nil||!r.Current||r.CancelRequested||s.Evidence.Store==nil{return empty,noManifest,nil,nil,v2input.ErrExpired}
 plan,err:=s.Evidence.Load(ctx,r);if err!=nil{return empty,noManifest,nil,nil,err}
 before,err:=s.row(ctx,r);if err!=nil{return empty,noManifest,nil,nil,err}
 raw,err:=readAuthoredRegionDocument(ctx,s.Evidence.Objects,before.Root,v2input.Ref{ArtifactID:before.Canonical.Root,SHA256:before.Canonical.RootSHA});if err!=nil{return empty,noManifest,nil,nil,err}
 if string(raw)!=before.Manifest{return empty,noManifest,nil,nil,ErrRegionInvalid}
 manifest,err:=parseAuthoredCropManifest(raw,r,plan,before);if err!=nil{return empty,noManifest,nil,nil,err}
 body,err:=readAuthoredDerivative(ctx,s.Evidence.Objects,before.Derivative,before.DerivativeRef);if err!=nil{return empty,noManifest,nil,nil,err}
 after,err:=s.row(ctx,r);if err!=nil{return empty,noManifest,nil,nil,err};if after!=before{return empty,noManifest,nil,nil,v2input.ErrConflict}
 latest,err:=s.Evidence.Store.Read(ctx,auth.Identity{UserID:r.Intent.Actor,SessionID:r.Intent.Session},r.Intent.Tenant,r.Intent.Asset,r.ID)
 if err!=nil{return empty,noManifest,nil,nil,err};if latest.validate()!=nil||latest.SHA!=r.SHA||latest.Revision!=r.Revision||latest.CropRun!=r.CropRun||latest.RenderBatch!=r.RenderBatch||!latest.Current||latest.CancelRequested{return empty,noManifest,nil,nil,v2input.ErrExpired}
 return before,manifest,raw,body,nil
}
func(s SQLAuthoredCropVerifier)Verify(ctx context.Context,r AuthoredRegionRecord)(AuthoredCropVerification,error){
 row,_,_,_,err:=s.load(ctx,r);return row.Canonical,err
}

// Read implements the authenticated derivative transport boundary. All physical
// IO and canonical receipt checks precede the final current-authority read.
func(s SQLAuthoredCropVerifier)Read(ctx context.Context,who auth.Identity,tenant,asset,request uuid.UUID)(AuthoredRegionMedia,error){
 if s.Evidence.Store==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||tenant==uuid.Nil||asset==uuid.Nil||request==uuid.Nil{return AuthoredRegionMedia{},ErrRegionInvalid}
 r,err:=s.Evidence.Store.Read(ctx,who,tenant,asset,request);if err!=nil{return AuthoredRegionMedia{},err}
 if r.Intent.Tenant!=tenant||r.Intent.Asset!=asset||r.Intent.Actor!=who.UserID||r.Intent.Session!=who.SessionID||r.ID!=request{return AuthoredRegionMedia{},ErrRegionInvalid}
 row,manifest,raw,body,err:=s.load(ctx,r);if err!=nil{return AuthoredRegionMedia{},err}
 return FreezeAuthoredRegionMedia(AuthoredRegionMedia{
  Proof:AuthoredRegionMediaProof{Tenant:tenant,Asset:asset,Actor:who.UserID,Session:who.SessionID,Revision:r.Revision,Canonical:row.Canonical},
  Descriptor:AuthoredRegionMediaDescriptor{SchemaVersion:1,Request:r.ID,Revision:r.Revision,IntentSHA:r.SHA,Manifest:v2input.Ref{ArtifactID:row.Canonical.Root,SHA256:row.Canonical.RootSHA},ManifestBytes:raw,Derivative:row.DerivativeRef,Bytes:row.Derivative.Bytes,MediaType:row.Derivative.ContentType,ReviewRequired:manifest.Review},Body:body,
 })
}
// Recheck performs no object IO. It revalidates the exact current request and
// immutable canonical tuple after response buffers have been frozen.
func(s SQLAuthoredCropVerifier)Recheck(ctx context.Context,who auth.Identity,tenant,asset,request uuid.UUID,proof AuthoredRegionMediaProof)error{
 if s.Evidence.Store==nil||proof.Tenant!=tenant||proof.Asset!=asset||proof.Actor!=who.UserID||proof.Session!=who.SessionID||proof.Canonical.Request!=request||proof.Revision<1{return ErrRegionInvalid}
 r,err:=s.Evidence.Store.Read(ctx,who,tenant,asset,request);if err!=nil{return err}
 if r.validate()!=nil||!r.Current||r.CancelRequested||r.Revision!=proof.Revision||r.SHA!=proof.Canonical.IntentSHA||r.Intent.Tenant!=tenant||r.Intent.Asset!=asset||r.Intent.Actor!=who.UserID||r.Intent.Session!=who.SessionID{return v2input.ErrExpired}
 row,err:=s.row(ctx,r);if err!=nil{return err};if row.Canonical!=proof.Canonical{return v2input.ErrConflict};return nil
}
