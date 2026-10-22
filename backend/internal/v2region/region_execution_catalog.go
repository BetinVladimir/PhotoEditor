package v2region

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// The catalog obtains current SQL authority separately from bounded immutable
// object IO. No pool connection or transaction spans object downloads.
type regionExecutionCatalog struct {
 API *pgxpool.Pool
 Objects RegionObjectReader
 Bucket string
 Tenant,Asset,Version,Actor,Session uuid.UUID
 Source,Style v2input.Ref
}

type regionExecutionCandidate struct {
 RunID uuid.UUID `json:"run_id"`
 Target v2input.Ref `json:"target"`
 CullingAuthority json.RawMessage `json:"culling_authority"`
 Stages []struct {
  TaskID uuid.UUID `json:"task_id"`
  ResultID uuid.UUID `json:"result_id"`
  Operation string `json:"operation"`
  Phase string `json:"phase"`
  ReviewAuthority json.RawMessage `json:"review_authority"`
  OperationVersion string `json:"operation_version"`
  Attempt int `json:"attempt"`
  Fence int64 `json:"fencing_token"`
  Params v2input.Ref `json:"params"`
  Inputs []v2input.Ref `json:"inputs"`
  Outputs []v2input.Ref `json:"outputs"`
 } `json:"stages"`
}

func (c regionExecutionCatalog) candidates(ctx context.Context)([]regionExecutionCandidate,error){
 if c.API==nil||c.Objects==nil||c.Bucket==""{return nil,ErrRegionInvalid}
 var raw []byte
 err:=database.TenantTransaction(ctx,c.API,c.Tenant,func(tx pgx.Tx)error{
  return tx.QueryRow(ctx,"SELECT photo.v26_region_execution_candidates($1,$2,$3,$4,$5,$6,$7,$8,$9)",c.Tenant,c.Asset,c.Version,c.Source.ArtifactID,c.Source.SHA256,c.Style.ArtifactID,c.Style.SHA256,c.Actor,c.Session).Scan(&raw)
 })
 if err!=nil{return nil,err}
 if len(raw)<2||len(raw)>1048576{return nil,ErrRegionUnavailable}
 var out []regionExecutionCandidate
 if json.Unmarshal(raw,&out)!=nil||len(out)>16{return nil,ErrRegionInvalid}
 for _,candidate:=range out{if candidate.RunID==uuid.Nil||!regionExecutionRef(candidate.Target)||len(candidate.Stages)<1||len(candidate.Stages)>128{return nil,ErrRegionInvalid}}
 return out,nil
}

func(c regionExecutionCatalog) document(ctx context.Context,run,task uuid.UUID,ref v2input.Ref)(objectstore.Object,string,error){
 var object objectstore.Object;var project,id uuid.UUID;var bucket,sha,role string
 if !regionExecutionRef(ref){return object,"",ErrRegionInvalid}
 err:=database.TenantTransaction(ctx,c.API,c.Tenant,func(tx pgx.Tx)error{
  return tx.QueryRow(ctx,"SELECT * FROM photo.v26_region_execution_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)",c.Tenant,c.Asset,c.Version,c.Source.ArtifactID,c.Source.SHA256,c.Style.ArtifactID,c.Style.SHA256,c.Actor,c.Session,run,task,ref.ArtifactID,ref.SHA256).Scan(&project,&id,&sha,&bucket,&object.Key,&object.VersionID,&object.Bytes,&object.ContentType,&role)
 })
 if err!=nil{return object,"",err}
 if project==uuid.Nil||id!=ref.ArtifactID||sha!=ref.SHA256||bucket!=c.Bucket||object.Key==""||object.VersionID==""||object.VersionID=="null"||object.Bytes<1{return object,"",ErrRegionInvalid}
 object.SHA256=sha
 return object,role,nil
}

func(c regionExecutionCatalog) producerParams(ctx context.Context,run,task uuid.UUID,plan v2input.Ref)(v2input.Ref,error){
 var raw []byte;var ref v2input.Ref
 err:=database.TenantTransaction(ctx,c.API,c.Tenant,func(tx pgx.Tx)error{
  return tx.QueryRow(ctx,"SELECT photo.v26_region_execution_producer_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)",c.Tenant,c.Asset,c.Version,c.Source.ArtifactID,c.Source.SHA256,c.Style.ArtifactID,c.Style.SHA256,c.Actor,c.Session,run,task,plan.ArtifactID,plan.SHA256).Scan(&raw)
 })
 if err!=nil{return ref,err};if len(raw)>1024||json.Unmarshal(raw,&ref)!=nil||!regionExecutionRef(ref){return ref,ErrRegionInvalid}
 return ref,nil
}

func(c regionExecutionCatalog) records(ctx context.Context,candidate regionExecutionCandidate)([]RegionExecutionRecord,error){
 records:=[]RegionExecutionRecord{}
 for _,row:=range candidate.Stages{
  if err:=ctx.Err();err!=nil{return nil,err}
  if row.OperationVersion!="1"||row.TaskID==uuid.Nil||row.ResultID==uuid.Nil||row.Attempt<1||row.Fence<1||len(row.Inputs)>512||len(row.Outputs)>512{return nil,ErrRegionInvalid}
  paramsObject,_,err:=c.document(ctx,candidate.RunID,row.TaskID,row.Params);if err!=nil{return nil,err}
  params,err:=readRegionDocument(ctx,c.Objects,paramsObject,row.Params);if err!=nil{return nil,err}
  var output v2input.Ref;var body []byte;var report *v2input.Ref
  for _,ref:=range row.Outputs{
   object,role,err:=c.document(ctx,candidate.RunID,row.TaskID,ref);if err!=nil{return nil,err}
   if role=="body_report" && row.Operation=="geometry.body" {
    if report!=nil{return nil,ErrRegionInvalid};owned:=ref;report=&owned
   }
   if role!="working_image"{continue}
   if output.ArtifactID!=uuid.Nil{return nil,ErrRegionInvalid}
   output=ref;body,err=readRegionDocument(ctx,c.Objects,object,ref);if err!=nil{return nil,err}
  }
  if output.ArtifactID==uuid.Nil{return nil,ErrRegionInvalid}
  var plan,policy *v2input.Ref
  if row.Operation=="geometry.crop"||row.Operation=="image.outpaint" {
   var parsed struct{Plan v2input.Ref `json:"plan"`}
   if row.Operation=="image.outpaint"{
    var raw []byte
    err:=database.TenantTransaction(ctx,c.API,c.Tenant,func(tx pgx.Tx)error{return tx.QueryRow(ctx,"SELECT photo.v26_region_execution_outpaint_plan($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)",c.Tenant,c.Asset,c.Version,c.Source.ArtifactID,c.Source.SHA256,c.Style.ArtifactID,c.Style.SHA256,c.Actor,c.Session,candidate.RunID,row.TaskID).Scan(&raw)})
    if err!=nil{return nil,err};if len(raw)>1024||json.Unmarshal(raw,&parsed.Plan)!=nil||!regionExecutionRef(parsed.Plan){return nil,ErrRegionInvalid}
   }else{
    if json.Unmarshal(params,&parsed)!=nil||!regionExecutionRef(parsed.Plan){return nil,ErrRegionInvalid}
    declared:=false;for _,input:=range row.Inputs{if input==parsed.Plan{declared=true}};if !declared{return nil,ErrRegionInvalid}
   }
   planObject,role,err:=c.document(ctx,candidate.RunID,row.TaskID,parsed.Plan);if err!=nil{return nil,err}
   if role!="composition_plan"{return nil,ErrRegionInvalid}
   if _,err=readRegionDocument(ctx,c.Objects,planObject,parsed.Plan);err!=nil{return nil,err}
   producer,err:=c.producerParams(ctx,candidate.RunID,row.TaskID,parsed.Plan);if err!=nil{return nil,err}
   policyObject,_,err:=c.document(ctx,candidate.RunID,row.TaskID,producer);if err!=nil{return nil,err}
   if _,err=readRegionDocument(ctx,c.Objects,policyObject,producer);err!=nil{return nil,err}
   owned:=parsed.Plan;plan=&owned;policy=&producer
  }
  stage:=RegionExecutionStage{Operation:row.Operation,OperationVersion:1,TaskID:row.TaskID,ResultID:row.ResultID,Attempt:row.Attempt,FencingToken:row.Fence,Params:row.Params,Output:output,Report:report,Plan:plan,PolicyParams:policy}
  if row.Operation=="image.generate"||row.Operation=="image.outpaint"{stage.Phase=row.Phase}
  if len(row.ReviewAuthority)>0&&string(row.ReviewAuthority)!="null"{stage.ReviewAuthority=row.ReviewAuthority}
  if (row.Operation=="defects.repair"||row.Operation=="sensor.dust_repair")&&len(stage.ReviewAuthority)==0{return nil,ErrRegionUnavailable}
  if row.Operation=="image.local"||row.Operation=="portrait.detail"||row.Operation=="sensor.dust_auto_repair"||row.Operation=="image.inpaint"||row.Operation=="image.generate"{
   var masks struct{Regions []struct{Invert bool `json:"invert"`} `json:"regions"`;Protected []json.RawMessage `json:"protected"`;Person json.RawMessage `json:"person"`}
   if json.Unmarshal(params,&masks)!=nil{return nil,ErrRegionInvalid}
   stage.InputMaskCount=int64(len(masks.Regions)+len(masks.Protected))
   if row.Operation=="image.local"{for _,region:=range masks.Regions{if region.Invert{stage.InputMaskCount++}};if len(masks.Person)>0&&string(masks.Person)!="null"{stage.InputMaskCount++}}
  }
  if row.Operation=="image.inpaint"||row.Operation=="image.generate"{stage.InputMaskCount++}
  if row.Operation=="defects.auto_repair"||row.Operation=="defects.repair"{stage.InputMaskCount=1}
  if row.Operation=="background.compose"{
   var parsed struct{Mode string `json:"mode"`;Background *v2input.Ref `json:"background"`}
   if json.Unmarshal(params,&parsed)!=nil{return nil,ErrRegionInvalid}
   if parsed.Mode=="asset"{
    if parsed.Background==nil||!regionExecutionRef(*parsed.Background){return nil,ErrRegionInvalid}
    var raw []byte
    err:=database.TenantTransaction(ctx,c.API,c.Tenant,func(tx pgx.Tx)error{return tx.QueryRow(ctx,"SELECT photo.v26_region_execution_background($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)",c.Tenant,c.Asset,c.Version,c.Source.ArtifactID,c.Source.SHA256,c.Style.ArtifactID,c.Style.SHA256,c.Actor,c.Session,candidate.RunID,row.TaskID,parsed.Background.ArtifactID,parsed.Background.SHA256).Scan(&raw)})
    var proof struct{Asset review.ApplyBackgroundAsset `json:"asset"`;Params v2input.Ref `json:"params"`}
    if err!=nil{return nil,err};if len(raw)>4096||json.Unmarshal(raw,&proof)!=nil||!regionExecutionRef(proof.Params)||proof.Asset.Style.ID!=c.Style.ArtifactID||proof.Asset.Style.SHA256!=c.Style.SHA256||proof.Asset.StyleVersionID==uuid.Nil{return nil,ErrRegionInvalid}
    stage.BackgroundAsset=&proof.Asset;stage.BackgroundParams=&proof.Params;stage.BackgroundImage=parsed.Background
    object,_,err:=c.document(ctx,candidate.RunID,row.TaskID,*parsed.Background);if err!=nil{return nil,err}
    decodedRaw,err:=readRegionDocument(ctx,c.Objects,object,*parsed.Background);if err!=nil{return nil,err}
    decoded,err:=review.ParseWorkingImage(decodedRaw);if err!=nil{return nil,err}
    if decoded.Alpha!=nil||decoded.Canvas.SourceSHA256!=proof.Asset.Source.SHA256{return nil,ErrRegionInvalid}
    stage.BackgroundCanvas=&decoded.Canvas
   }
  }
  records=append(records,RegionExecutionRecord{Stage:stage,ParamsBody:params,OutputBody:body})
 }
 return records,nil
}
