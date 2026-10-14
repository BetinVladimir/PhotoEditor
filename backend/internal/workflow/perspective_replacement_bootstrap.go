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

func decodePerspectiveSchedulerDeployment(raw []byte,pin string)(v2optical.PerspectiveDeployment,error){
 var zero v2optical.PerspectiveDeployment
 if len(raw)<2||len(raw)>8<<20{return zero,errors.New("perspective replacement configuration size invalid")}
 if _,err:=domain.ParseDigest(pin);err!=nil{return zero,errors.New("perspective replacement configuration digest unavailable")}
 digest:=sha256.Sum256(raw)
 if hex.EncodeToString(digest[:])!=pin{return zero,errors.New("perspective replacement configuration digest mismatch")}
 deployment,err:=v2optical.DecodePerspectiveDeployment(raw)
 if err!=nil{return zero,errors.New("perspective replacement configuration invalid")}
 return deployment,nil
}

func buildPerspectiveReplacementProjects(ctx context.Context,pool *pgxpool.Pool,objects *objectstore.Store,bucket string)(*v2optical.PerspectiveProjects,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 path:=os.Getenv("PHOTO_V29_PERSPECTIVE_CONFIG")
 if path==""{return nil,nil}
 if pool==nil||objects==nil||len(bucket)<3||len(bucket)>63{return nil,errors.New("perspective replacement dependencies unavailable")}
 file,err:=os.Open(path);if err!=nil{return nil,errors.New("perspective replacement configuration unavailable")};defer file.Close()
 info,err:=file.Stat();if err!=nil||!info.Mode().IsRegular()||info.Size()>8<<20{return nil,errors.New("perspective replacement configuration invalid")}
 raw,err:=io.ReadAll(io.LimitReader(file,(8<<20)+1));if err!=nil{return nil,errors.New("perspective replacement configuration read failed")}
 deployment,err:=decodePerspectiveSchedulerDeployment(raw,os.Getenv("PHOTO_V29_PERSPECTIVE_CONFIG_SHA256"));if err!=nil{return nil,err}
 required:=perspectiveReplacementRequiredFunctions()
 startup,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 var role string;var installed bool
 if err=pool.QueryRow(startup,`SELECT current_user,coalesce(bool_and(to_regprocedure(signature) IS NOT NULL AND has_function_privilege(current_user,to_regprocedure(signature),'EXECUTE')),false) FROM unnest($1::text[]) signature`,required).Scan(&role,&installed);err!=nil||role!="photo_scheduler"||!installed{return nil,errors.New("perspective replacement installation unavailable")}
 if err=verifyPerspectiveReplacementExecutionInstallation(ctx,pool);err!=nil{return nil,err}
 projects,err:=deployment.Build(pool,objects,bucket);if err!=nil{return nil,errors.New("perspective replacement deployment refused")}
 return projects,ctx.Err()
}

// Include downstream observations: an admitted task must be observable through
// preview and array authority using its replacement identity.
func perspectiveReplacementRequiredFunctions() []string {
 return []string{
 "photo.save_v29_manual_perspective_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text)",
 "photo.read_v29_manual_perspective_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid)",
 "photo.probe_v29_manual_perspective_replacement_publication(uuid,uuid,uuid,uuid,text)",
 "photo.reserve_v29_manual_perspective_replacement_params_document(uuid,uuid,uuid,text,text,bigint,text,jsonb)",
 "photo.complete_v29_manual_perspective_replacement_params_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb)",
 "photo.stage_v29_manual_perspective_replacement_task(uuid,uuid,text,jsonb)",
 "photo.reserve_v29_manual_perspective_replacement_transport_document(uuid,uuid,uuid,text,text,bigint,text,jsonb,text)",
 "photo.complete_v29_manual_perspective_replacement_transport_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb,text)",
 "photo.read_v29_manual_perspective_replacement_published_transport(uuid,uuid,uuid,text,jsonb)",
 "photo.register_v29_manual_perspective_replacement_transport(uuid,uuid,text,jsonb,jsonb,text)",
 "photo.seal_v29_manual_perspective_replacement_graph(uuid,uuid,uuid,text,jsonb)",
 "photo.read_v29_manual_perspective_replacement_queued_receipt(uuid,uuid,uuid,uuid,text)",
 "photo.read_v29_perspective_preview_replacement_identity(uuid,uuid)",
 "photo.v29_perspective_preview_original_array(uuid,uuid,uuid,integer,bigint,text,uuid,text,text)",
 }
}
