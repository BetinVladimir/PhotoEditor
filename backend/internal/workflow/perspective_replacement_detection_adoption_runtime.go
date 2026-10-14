package workflow

import (
 "context"
 "errors"
 "path/filepath"
 "strconv"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/objectstore"
)

// Replacement adoption is independently enabled: ordinary detector adoption
// must never substitute its authority for a replacement detector.
func ReplacementDetectionAdoptionFromEnvironment(getenv func(string)string)(PythonReplacementDetectionAdopter,bool,error){
 var out PythonReplacementDetectionAdopter
 if getenv==nil{return out,false,errors.New("replacement adoption environment unavailable")}
 flag:=getenv("PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_ADOPTION_ENABLED")
 if flag==""||flag=="false"{return out,false,nil}
 if flag!="true"{return out,false,errors.New("replacement adoption enabled flag invalid")}
 out.Python=getenv("PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_PYTHON")
 out.WorkerRoot=getenv("PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_WORKER_ROOT")
 out.BridgeRoot=getenv("PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_BRIDGE_ROOT")
 for _,path:=range []string{out.Python,out.WorkerRoot,out.BridgeRoot}{if !filepath.IsAbs(path)||filepath.Clean(path)!=path{return out,false,errors.New("replacement adoption path invalid")}}
 seconds:=int64(600)
 if raw:=getenv("PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_TIMEOUT_SECONDS");raw!=""{n,err:=strconv.ParseInt(raw,10,64);if err!=nil||n<1||n>600{return out,false,errors.New("replacement adoption timeout invalid")};seconds=n}
 out.Timeout=time.Duration(seconds)*time.Second
 out.Environment=map[string]string{}
 for _,key:=range []string{"DATABASE_URL","S3_ENDPOINT","S3_BUCKET","S3_ACCESS_KEY","S3_SECRET_KEY","APP_ENV","PHOTO_ICC_ROOT","PHOTO_ADOPTER_PERSPECTIVE_LINES_ENABLED","PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG","PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG_SHA","PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_ROOT","PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_MAX_BYTES","PHOTO_ADOPTER_PERSPECTIVE_LINES_RAM_MAX_BYTES"}{
  value:=getenv(key)
  if value==""&&key!="APP_ENV"{return out,false,errors.New("replacement adoption operator configuration incomplete")}
  if value!=""{out.Environment[key]=value}
 }
 if out.Environment["PHOTO_ADOPTER_PERSPECTIVE_LINES_ENABLED"]!="true"||domainDigestInvalid(out.Environment["PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG_SHA"]){return out,false,errors.New("replacement adoption detector installation invalid")}
 for _,key:=range []string{"PHOTO_ICC_ROOT","PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG","PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_ROOT"}{if !filepath.IsAbs(out.Environment[key]){return out,false,errors.New("replacement adoption installation path invalid")}}
 for _,key:=range []string{"PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_MAX_BYTES","PHOTO_ADOPTER_PERSPECTIVE_LINES_RAM_MAX_BYTES"}{n,err:=strconv.ParseInt(out.Environment[key],10,64);if err!=nil||n<=0{return out,false,errors.New("replacement adoption resource limit invalid")}}
 return out,true,nil
}

func NewReplacementDetectionAdoptionProcessor(ctx context.Context,pool *pgxpool.Pool,objects *objectstore.Store,bucket string,runner PythonReplacementDetectionAdopter)(*ReplacementDetectionAdoptionDispatcher,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if pool==nil||objects==nil||len(bucket)<3||len(bucket)>63||runner.Environment["S3_BUCKET"]!=bucket||runner.Timeout<time.Second||runner.Timeout>10*time.Minute{return nil,errors.New("replacement adoption dependencies invalid")}
 bounded,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 required:=[]string{
 "photo.next_v29_replacement_detector_adoption(uuid)",
 "photo.read_v29_replacement_detector_decision(uuid,uuid,text,uuid)",
 "photo.v29_replacement_detector_adoption_evidence(uuid,uuid,integer,bigint,uuid,text,jsonb)",
 "photo.adopt_v29_replacement_detector(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb)",
 "photo.v29_replacement_detector_adoption_committed(uuid,uuid,text,uuid)",
 }
 var installed bool
 err:=pool.QueryRow(bounded,`SELECT current_user='photo_scheduler' AND coalesce(bool_and(coalesce(p.prosecdef AND owner.rolname='photo_fn_owner' AND has_function_privilege(current_user,p.oid,'EXECUTE'),false)),false)
 FROM unnest($1::text[]) required(signature)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(signature)
 LEFT JOIN pg_roles owner ON owner.oid=p.proowner`,required).Scan(&installed)
 if err!=nil||!installed{return nil,errors.New("replacement adoption SQL installation unavailable")}
 return &ReplacementDetectionAdoptionDispatcher{Candidates:ReplacementDetectionAdoptionQueue{DB:pool},Loader:ReplacementDetectionAdoptionLoader{DB:pool,Reader:objects,Bucket:bucket},Runner:runner,Timeout:runner.Timeout},ctx.Err()
}
