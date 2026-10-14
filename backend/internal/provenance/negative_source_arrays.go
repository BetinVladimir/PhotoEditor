package provenance

import (
 "bytes"
 "context"
 "encoding/json"
 "strconv"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type SigningNegativeSourceArrayCatalog interface{
 LookupSigningNegativeSourceArray(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask,SigningNegativePixelEvidence,string)(objectstore.Object,error)
}
type SQLSigningNegativeSourceArrayCatalog struct{
 DB *pgxpool.Pool
 Bucket string
 Adoption SigningNegativeAdoptionReader
 Documents SigningTaskDocumentReader
}

func(s SQLSigningNegativeSourceArrayCatalog)LookupSigningNegativeSourceArray(ctx context.Context,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask,evidence SigningNegativePixelEvidence,role string)(objectstore.Object,error){
 var zero objectstore.Object
 if err:=ctx.Err();err!=nil{return zero,err}
 sha,err:=canonical.Hash(task)
 if err!=nil||sha!=evidence.taskDigest||task.Operation!="optical.negative"||task.Version!="1"||s.DB==nil||s.Adoption==nil||s.Documents==nil||len(s.Bucket)<3||!text(s.Bucket,63){return zero,ErrInvalid}
 width,height:=evidence.source.Canvas.Width,evidence.source.Canvas.Height
 if width<1||height<1||width>200000000||height>200000000/width{return zero,ErrInvalid}
 spec,err:=signingNegativeSourceArraySpec(evidence,role);if err!=nil{return zero,err}
 pin,size:=spec.pin,4*spec.width*spec.height*int64(spec.channels)
 if !ref(pin)||size<4||size>2400000000{return zero,ErrInvalid}
 check:=func()error{
  root,err:=s.Adoption.ReadSigningAdoptedNegativeRoot(ctx,who,ready,task);if err!=nil{return err};if root!=evidence.root{return ErrInvalid}
  paramsRaw,err:=s.Documents.ReadSigningTaskDocument(ctx,who,ready,task,task.Params,65536);if err!=nil{return err}
  if !pinned(paramsRaw,task.Params,65536){return ErrInvalid};params,err:=v2optical.ParseNegativeParams(paramsRaw);if err!=nil||params!=evidence.params{return ErrInvalid}
  raw,err:=s.Documents.ReadSigningTaskDocument(ctx,who,ready,task,params.Base.Source,65536);if err!=nil{return err}
  if !pinned(raw,params.Base.Source,65536){return ErrInvalid}
  source,err:=v2optical.VerifyNegativeWorkingImage(params,raw);if err!=nil{return err}
  actual,err:=canonical.Marshal(source);if err!=nil{return err};expected,err:=canonical.Marshal(evidence.source)
  if err!=nil||!bytes.Equal(actual,expected){return ErrInvalid};return ctx.Err()
 }
 if err:=check();err!=nil{return zero,err}
 var object objectstore.Object
 err=tenant.WithTenant(ctx,s.DB,who,ready.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var bucket string
  err:=tx.QueryRow(ctx,`SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
   FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
   JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id AND w.run_id=$2 AND w.project_id=i.project_id
   WHERE i.tenant_id=$1 AND i.task_id=$3 AND i.project_id=$4 AND i.contract='NpyArray'
   AND i.artifact_id=$5 AND i.sha256=$6 AND a.bucket=$7
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND a.media_type='application/x-npy' AND a.bytes BETWEEN $8 AND $8+65536
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND EXISTS(SELECT 1 FROM photo.v2_task_inputs parent WHERE parent.tenant_id=i.tenant_id
    AND parent.task_id=i.task_id AND parent.project_id=i.project_id AND parent.contract='WorkingImage'
    AND parent.artifact_id=$9 AND parent.sha256=$10) FOR SHARE OF a`,ready.Tenant,task.Run,task.Task,ready.Project,pin.ArtifactID,pin.SHA256,s.Bucket,size,evidence.params.Base.Source.ArtifactID,evidence.params.Base.Source.SHA256).Scan(&bucket,&object.Key,&object.VersionID,&object.SHA256,&object.Bytes,&object.ContentType)
  if err!=nil{return err};if bucket!=s.Bucket{return ErrInvalid};return nil
 })
 if err!=nil{return zero,err};if err:=check();err!=nil{return zero,err};return object,nil
}

type BoundSigningNegativeSourceArraysVerifier struct{
 Catalog SigningNegativeSourceArrayCatalog
 Objects SigningObjects
}
func(s BoundSigningNegativeSourceArraysVerifier)Verify(ctx context.Context,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask,evidence SigningNegativePixelEvidence)error{
 if err:=ctx.Err();err!=nil{return err}
 sha,err:=canonical.Hash(task)
 if err!=nil||sha!=evidence.taskDigest||s.Catalog==nil||s.Objects==nil||task.Operation!="optical.negative"||task.Version!="1"{return ErrInvalid}
 width,height:=evidence.source.Canvas.Width,evidence.source.Canvas.Height
 if width<1||height<1||width>200000000||height>200000000/width{return ErrInvalid}
 roles:=[]string{"pixels"};if evidence.source.Alpha!=nil{roles=append(roles,"alpha")}
 fields,err:=signingNegativeInverseFields(evidence);if err!=nil{return err}
 for i,field:=range fields{if field.Inverse!=nil{roles=append(roles,"InverseField/"+strconv.Itoa(i))}}
 for _,role:=range roles{
  spec,err:=signingNegativeSourceArraySpec(evidence,role);if err!=nil{return err}
  object,err:=s.Catalog.LookupSigningNegativeSourceArray(ctx,who,ready,task,evidence,role);if err!=nil{return err}
  expected,size:=spec.pin.SHA256,4*spec.width*spec.height*int64(spec.channels)
  if object.Key==""||object.VersionID==""||object.VersionID=="null"||object.SHA256!=expected||object.ContentType!="application/x-npy"||object.Bytes<size||object.Bytes>size+65536{return ErrInvalid}
  stream,err:=s.Objects.Open(ctx,object);if err!=nil{if stream!=nil{_ = stream.Close()};return err};if stream==nil{return ErrInvalid}
  var verifyErr error
  if role=="alpha"{verifyErr=masknpy.VerifyAlphaNPY(ctx,stream,object.Bytes,object.SHA256,spec.width,spec.height)}else{verifyErr=masknpy.VerifyFloat32ArrayNPY(ctx,stream,object.Bytes,object.SHA256,spec.width,spec.height,spec.channels,false)}
  closeErr:=stream.Close();if err:=ctx.Err();err!=nil{return err};if verifyErr!=nil{return verifyErr};if closeErr!=nil{return closeErr}
  current,err:=s.Catalog.LookupSigningNegativeSourceArray(ctx,who,ready,task,evidence,role);if err!=nil{return err};if current!=object{return ErrInvalid}
 }
 return ctx.Err()
}

var _ SigningNegativeSourceArrayCatalog=SQLSigningNegativeSourceArrayCatalog{}

// Keep every dense-warp occurrence: shared artifacts must satisfy each step's shape.
type signingNegativeInverseField struct{
 Kind string `json:"kind"`
 To struct{Width int64 `json:"width"`;Height int64 `json:"height"`} `json:"to_canvas"`
 Inverse *v2input.Ref `json:"inverse_field"`
}
type signingNegativeArraySpec struct{pin v2input.Ref;width,height int64;channels int}
func signingNegativeInverseFields(e SigningNegativePixelEvidence)([]signingNegativeInverseField,error){
 if len(e.source.TransformChain)==0{return nil,nil}
 if len(e.source.TransformChain)>65536{return nil,ErrInvalid}
 var chain struct{Steps []signingNegativeInverseField `json:"steps"`}
 if json.Unmarshal(e.source.TransformChain,&chain)!=nil||len(chain.Steps)>128{return nil,ErrInvalid}
 for _,step:=range chain.Steps{
  if step.Kind=="dense_warp"&&(step.Inverse==nil||!ref(*step.Inverse)){return nil,ErrInvalid}
  if step.Inverse!=nil&&(step.Kind!="dense_warp"||!ref(*step.Inverse)||step.To.Width<1||step.To.Height<1||step.To.Width>200000000/step.To.Height){return nil,ErrInvalid}
 }
 return chain.Steps,nil
}
func signingNegativeSourceArraySpec(e SigningNegativePixelEvidence,role string)(signingNegativeArraySpec,error){
 spec:=signingNegativeArraySpec{pin:v2input.Ref{ArtifactID:e.source.Pixels.ID,SHA256:e.source.Pixels.SHA256},width:e.source.Canvas.Width,height:e.source.Canvas.Height,channels:3}
 if role=="pixels"{return spec,nil}
 if role=="alpha"&&e.source.Alpha!=nil{spec.pin=v2input.Ref{ArtifactID:e.source.Alpha.ID,SHA256:e.source.Alpha.SHA256};spec.channels=1;return spec,nil}
 fields,err:=signingNegativeInverseFields(e);if err!=nil{return signingNegativeArraySpec{},err}
 for i,field:=range fields{if field.Inverse!=nil&&role=="InverseField/"+strconv.Itoa(i){return signingNegativeArraySpec{pin:*field.Inverse,width:field.To.Width,height:field.To.Height,channels:2},nil}}
 return signingNegativeArraySpec{},ErrInvalid
}
