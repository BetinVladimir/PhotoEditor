package workflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "io"
 "os"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2optical"
)

func decodeNegativeSchedulerDeployment(raw []byte,expected string)(v2optical.NegativeDeployment,error){
 var zero v2optical.NegativeDeployment
 if len(raw)<2||len(raw)>8<<20{return zero,errors.New("negative replacement configuration size invalid")}
 if _,err:=domain.ParseDigest(expected);err!=nil{return zero,errors.New("negative replacement configuration digest unavailable")}
 sum:=sha256.Sum256(raw)
 if hex.EncodeToString(sum[:])!=expected{return zero,errors.New("negative replacement configuration digest mismatch")}
 deployment,err:=v2optical.DecodeNegativeDeployment(raw);if err!=nil{return zero,errors.New("negative replacement configuration invalid")}
 return deployment,nil
}

// The optional deployment is the same digest-pinned project configuration used
// by the negative API. It does not synthesize reports, keys or session authority.
func buildNegativeReplacementScheduler(ctx context.Context,pool *pgxpool.Pool,objects *objectstore.Store,bucket string,workingICC ...string)(v2optical.NegativeSchedulerBackend,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 preparationFlag:=os.Getenv("PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_PREPARATION_ENABLED")
 if preparationFlag!=""&&preparationFlag!="false"&&preparationFlag!="true"{return nil,errors.New("replacement detector preparation flag invalid")}
 base:=v2optical.SQLNegativeScheduler{DB:pool}
 path:=os.Getenv("PHOTO_V29_NEGATIVE_CONFIG")
 if path==""{
  if os.Getenv("PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_PREPARATION_ENABLED")=="true"{return nil,errors.New("replacement detector requires negative deployment configuration")}
  if os.Getenv("PHOTO_V29_PERSPECTIVE_CONFIG")!=""||os.Getenv("PHOTO_V29_PANORAMA_CONFIG")!=""||os.Getenv("PHOTO_V29_FOCUS_STACK_CONFIG")!=""{return nil,errors.New("optical replacement requires negative deployment configuration")}
  return base,nil
 }
 if pool==nil||objects==nil||len(bucket)<3||len(bucket)>63{return nil,errors.New("negative replacement dependencies unavailable")}
 file,err:=os.Open(path);if err!=nil{return nil,errors.New("negative replacement configuration unavailable")}
 defer file.Close()
 info,err:=file.Stat();if err!=nil||!info.Mode().IsRegular()||info.Size()>8<<20{return nil,errors.New("negative replacement configuration invalid")}
 raw,err:=io.ReadAll(io.LimitReader(file,(8<<20)+1));if err!=nil{return nil,errors.New("negative replacement configuration read failed")}
 deployment,err:=decodeNegativeSchedulerDeployment(raw,os.Getenv("PHOTO_V29_NEGATIVE_CONFIG_SHA256"));if err!=nil{return nil,err}
 startup,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 required:=[]string{
  "photo.save_v29_negative_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text)",
  "photo.read_v29_negative_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid)",
  "photo.probe_v29_negative_replacement_publication(uuid,uuid,uuid,uuid,text)",
  "photo.stage_v29_negative_replacement_task(uuid,uuid,text,jsonb)",
  "photo.reserve_v29_negative_replacement_params_document(uuid,uuid,uuid,text,text,bigint,text,jsonb)",
  "photo.complete_v29_negative_replacement_params_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb)",
  "photo.reserve_v29_negative_replacement_transport_document(uuid,uuid,uuid,text,text,bigint,text,jsonb,text)",
  "photo.complete_v29_negative_replacement_transport_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb,text)",
  "photo.read_v29_negative_replacement_published_transport(uuid,uuid,uuid,text,jsonb)",
  "photo.register_v29_negative_replacement_transport(uuid,uuid,text,jsonb,jsonb,text)",
  "photo.seal_v29_negative_replacement_graph(uuid,uuid,uuid,text,jsonb)",
  "photo.read_v29_negative_replacement_queued_receipt(uuid,uuid,uuid,uuid,text)",
  "photo.read_v29_negative_adopted_replacements(uuid,uuid,uuid,uuid,uuid,text,jsonb)",
  "photo.read_v29_negative_replacement_snapshot(uuid,uuid,uuid,uuid,uuid,text,uuid)",
  "photo.lock_v29_negative_replacement_snapshot(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid)",
  "photo.read_v29_negative_replacement_identity(uuid,uuid,uuid,text)",
 }
 var role string;var installed bool
 if err=pool.QueryRow(startup,`SELECT current_user,coalesce(bool_and(to_regprocedure(signature) IS NOT NULL AND has_function_privilege(current_user,to_regprocedure(signature),'EXECUTE')),false) FROM unnest($1::text[]) signature`,required).Scan(&role,&installed);err!=nil||role!="photo_scheduler"||!installed{return nil,errors.New("negative replacement installation unavailable")}
 projects,err:=deployment.Build(pool,objects,bucket);if err!=nil{return nil,errors.New("negative replacement deployment refused")}
 perspective,err:=buildPerspectiveReplacementProjects(ctx,pool,objects,bucket);if err!=nil{return nil,err}
 var waves v2optical.NegativeReplacementWaveService=projects
 if perspective!=nil{waves=v2optical.OpticalReplacementDispatcher{Negative:projects,Perspective:perspective}}
 flag:=os.Getenv("PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_PREPARATION_ENABLED")
 if flag!=""&&flag!="false"&&flag!="true"{return nil,errors.New("replacement detector preparation flag invalid")}
 if flag=="true"{
  if err:=verifyAutomaticReplacementInstallation(ctx,pool);err!=nil{return nil,err}
  if len(workingICC)!=1{return nil,errors.New("replacement detector working ICC unavailable")}
  policy,err:=CompilePerspectiveLinesExecutionPolicy(workingICC[0],os.Getenv("PHOTO_SCHEDULER_PERSPECTIVE_LINES_RUNTIME_SHA"));if err!=nil{return nil,err}
  waves=ReplacementDetectionWaveDispatcher{Base:waves,Detection:ReplacementDetectionWaveService{AutomaticProjects:perspective,DB:pool,Reader:objects,Objects:objects,Bucket:bucket,Policy:policy}}
 }
 panorama,err:=buildMultiReplacementProjects(ctx,pool,objects,bucket,"optical.panorama");if err!=nil{return nil,err}
 focus,err:=buildMultiReplacementProjects(ctx,pool,objects,bucket,"optical.focus_stack");if err!=nil{return nil,err}
 if panorama!=nil||focus!=nil{
  dispatcher:=v2optical.MultiReplacementDispatcher{Base:waves}
  // Avoid typed nil interfaces: an absent operation must preserve deferred work.
  if panorama!=nil{dispatcher.Panorama=panorama}
  if focus!=nil{dispatcher.FocusStack=focus}
  waves=dispatcher
 }
 return v2optical.NegativeReplacementScheduler{Base:base,Projects:waves,Identities:v2optical.SQLNegativeReplacementIdentityResolver{DB:pool},Limit:1},nil
}
