package v2region

import (
 "context"
 "encoding/json"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2render"
)

// AuthoredRegionRuntime shares one immutable dependency graph across API,
// scheduler recovery and physical media reads. It owns no pools or credentials.
// Installation alone does not enable an operation or qualify a renderer.
type AuthoredRegionRuntime struct {
 Queue AuthoredRegionQueue
 Recovery AuthoredRegionRecovery
 Media SQLAuthoredCropVerifier
}

func NewSQLAuthoredRegionRuntime(ctx context.Context,api,scheduler *pgxpool.Pool,objects *objectstore.Store,bucket string,submission v2render.SQLRenderSubmissionService,policy SQLAuthoredRegionPolicy,profile []byte)(AuthoredRegionRuntime,error){
 var empty AuthoredRegionRuntime
 if api==nil||scheduler==nil||api==scheduler||objects==nil||len(bucket)<3||len(bucket)>63||submission.Factory.API!=api||submission.Factory.Scheduler!=scheduler||policy.Calibrations==nil||policy.Resolver.API!=api||submission.Factory.Sources!=objects||submission.Factory.Documents!=objects||submission.Factory.SourceBucket!=bucket||submission.Factory.DocumentBucket!=bucket||submission.Factory.QualificationPin!=nil||submission.Factory.SubmissionSHA!=""||submission.Factory.SubmissionKey!=""{return empty,ErrRegionUnavailable}
 if err:=requireAuthoredPoolRole(ctx,api,"photo_api");err!=nil{return empty,err}
 if err:=requireAuthoredPoolRole(ctx,scheduler,"photo_scheduler");err!=nil{return empty,err}
 if err:=RequireAuthoredRegionSchema(ctx,api,scheduler);err!=nil{return empty,err}
 if !validRegionSHA(policy.TargetICCSHA)||!validRegionSHA(policy.PresentationRuntimeSHA)||!validRegionSHA(policy.PresentationICCLockSHA){return empty,ErrRegionInvalid}
 raw,err:=validateAuthoredRuntimeProfile(policy,profile);if err!=nil{return empty,err}
 store:=SQLAuthoredRegionStore{API:api,Scheduler:scheduler}
 evidence:=SQLAuthoredRenderEvidence{API:api,Scheduler:scheduler,Objects:objects,Bucket:bucket,Store:store}
 verifier:=SQLAuthoredCropVerifier{Evidence:evidence}
 children:=SQLAuthoredRegionChildren{Scheduler:scheduler,Render:AuthoredRegionRender{Scheduler:scheduler,Submission:submission},Evidence:evidence,
  Params:v2draft.Publisher{DB:api,Objects:objects,Bucket:bucket},Documents:artifactjson.Publisher{Repository:artifactjson.SQLRepository{DB:scheduler,Bucket:bucket},Objects:objects},Profile:append([]byte(nil),raw...),CropVerifier:verifier}
 coordinator:=AuthoredRegionCoordinator{Store:store,Children:children}
 return AuthoredRegionRuntime{Queue:AuthoredRegionQueue{Preparation:SQLAuthoredRegionPreparation{API:api,Objects:objects,Bucket:bucket},Policy:policy,Coordinator:coordinator},Recovery:AuthoredRegionRecovery{Scheduler:scheduler,Children:children},Media:verifier},nil
}

func validateAuthoredRuntimeProfile(policy SQLAuthoredRegionPolicy,profile []byte)([]byte,error){
 raw,err:=canonical.Bytes(profile);if err!=nil||len(raw)>65536{return nil,ErrRegionInvalid}
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil||fields==nil{return nil,ErrRegionInvalid}
 value:=func(key string)string{var out string;if json.Unmarshal(fields[key],&out)!=nil{return ""};return out}
 if value("backend")!="cpu"||value("renderer_version")!="v2.2"||value("runtime_lock_sha")!=policy.PresentationRuntimeSHA||value("icc_lock_sha")!=policy.PresentationICCLockSHA{return nil,ErrRegionInvalid}
 // These are derived per request from the actual resolved closure. A static
 // server profile must never carry another request's transport or intent.
 for _,key:=range []string{"contracts_sha","v2_input_manifest","v2_entry_count","v2_entries_sha","authored_region_intent_sha256","authored_region_input_bytes","resource_estimate"}{if _,exists:=fields[key];exists{return nil,ErrRegionInvalid}}
 return append([]byte(nil),raw...),nil
}

// Recovery is deliberately not an API queue factory. It reads the immutable
// request's own actor, target ICC and qualification pin, including withdrawn
// requests that still need cancellation; no fabricated policy is supplied.
func NewSQLAuthoredRegionRecovery(ctx context.Context,api,scheduler *pgxpool.Pool,objects *objectstore.Store,bucket string,submission v2render.SQLRenderSubmissionService,installed AuthoredRegionInstallation)(AuthoredRegionRecovery,error){
 var empty AuthoredRegionRecovery
 if api==nil||scheduler==nil||api==scheduler||objects==nil||len(bucket)<3||len(bucket)>63||submission.Factory.API!=api||submission.Factory.Scheduler!=scheduler||submission.Factory.Sources!=objects||submission.Factory.Documents!=objects||submission.Factory.SourceBucket!=bucket||submission.Factory.DocumentBucket!=bucket||submission.Factory.QualificationPin!=nil||submission.Factory.SubmissionSHA!=""||submission.Factory.SubmissionKey!=""||submission.Calibrations==nil{return empty,ErrRegionUnavailable}
 if err:=RequireAuthoredRegionSchema(ctx,api,scheduler);err!=nil{return empty,err}
 profile,err:=installed.Profile();if err!=nil{return empty,err}
 store:=SQLAuthoredRegionStore{API:api,Scheduler:scheduler}
 evidence:=SQLAuthoredRenderEvidence{API:api,Scheduler:scheduler,Objects:objects,Bucket:bucket,Store:store}
 children:=SQLAuthoredRegionChildren{Scheduler:scheduler,Render:AuthoredRegionRender{Scheduler:scheduler,Submission:submission},Evidence:evidence,
  Params:v2draft.Publisher{DB:api,Objects:objects,Bucket:bucket},Documents:artifactjson.Publisher{Repository:artifactjson.SQLRepository{DB:scheduler,Bucket:bucket},Objects:objects},Profile:profile,CropVerifier:SQLAuthoredCropVerifier{Evidence:evidence}}
 return AuthoredRegionRecovery{Scheduler:scheduler,Children:children},nil
}
