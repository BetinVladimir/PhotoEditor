package workflow

import (
 "context"
 "errors"
 "strconv"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)
func MetadataAdoptionFromEnvironment(getenv func(string)string)(PythonMetadataAdopter,bool,error){
 var runner PythonMetadataAdopter
 if getenv==nil{return runner,false,errors.New("metadata adoption environment unavailable")}
 switch getenv("PHOTO_DELIVERY_METADATA_ADOPTION_ENABLED"){
 case "","false":return runner,false,nil
 case "true":
 default:return runner,false,errors.New("metadata adoption flag invalid")
 }
 runner.Python=getenv("PHOTO_DELIVERY_METADATA_ADOPTION_PYTHON")
 runner.WorkerRoot=getenv("PHOTO_DELIVERY_METADATA_ADOPTION_WORKER_ROOT")
 seconds:=int64(600)
 if raw:=getenv("PHOTO_DELIVERY_METADATA_ADOPTION_TIMEOUT_SECONDS");raw!=""{
  n,err:=strconv.ParseInt(raw,10,64);if err!=nil||n<1||n>600{return runner,false,errors.New("metadata adoption timeout invalid")};seconds=n
 }
 runner.Timeout=time.Duration(seconds)*time.Second;runner.Environment=map[string]string{}
 for _,key:=range metadataAdoptionEnvironmentKeys{if value:=getenv(key);value!=""{runner.Environment[key]=value}}
 if _,err:=runner.environment();err!=nil{return runner,false,errors.New("metadata adoption installation configuration invalid")}
 return runner,true,nil
}
func NewMetadataAdoptionProcessor(ctx context.Context,pool *pgxpool.Pool,bucket string,runner PythonMetadataAdopter)(*MetadataAdoptionDispatcher,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if pool==nil||runner.Environment["S3_BUCKET"]!=bucket{return nil,errors.New("metadata adoption dependencies invalid")}
 if _,err:=runner.environment();err!=nil{return nil,err}
 query,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 signatures:=[]string{
 "photo.next_v30_metadata_adoption(uuid)","photo.v30_metadata_adoption_committed(uuid,uuid,integer,bigint,uuid)",
 "photo.read_v30_metadata_adoption_input(uuid,uuid,integer,bigint,uuid)",
 "photo.v30_metadata_result_artifact(uuid,uuid,integer,bigint,uuid,uuid)","photo.v30_metadata_result_outputs(uuid,uuid,integer,bigint,uuid)",
 "photo.adopt_v30_delivery_metadata(uuid,uuid,integer,bigint,uuid,text,text,text,text,text,bigint)",
 "photo.settle_v30_metadata_invalid(uuid,uuid,integer,bigint,uuid,text)",
 }
 var installed bool
 err:=pool.QueryRow(query,`SELECT current_user='photo_scheduler' AND coalesce(bool_and(coalesce(p.prosecdef AND owner.rolname='photo_fn_owner' AND has_function_privilege(current_user,p.oid,'EXECUTE'),false)),false)
 FROM unnest($1::text[]) required(signature)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(signature)
 LEFT JOIN pg_roles owner ON owner.oid=p.proowner`,signatures).Scan(&installed)
 if err!=nil||!installed{return nil,errors.New("metadata adoption SQL installation unavailable")}
 // Own the explicit process environment independently of caller mutation.
 owned:=map[string]string{};for key,value:=range runner.Environment{owned[key]=value};runner.Environment=owned
 return &MetadataAdoptionDispatcher{Candidates:MetadataAdoptionQueue{DB:pool},Runner:runner,Timeout:runner.Timeout},ctx.Err()
}
