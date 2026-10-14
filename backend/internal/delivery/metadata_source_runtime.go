package delivery

import (
 "errors"
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "strconv"
 "strings"
 "time"
)

// MetadataSourceFromEnvironment installs explicit API-side extraction settings.
// Disabled mode requires no credentials and never enables public metadata modes.
func MetadataSourceFromEnvironment(getenv func(string)string)(PythonMetadataSourcePreparer,bool,error){
 var runner PythonMetadataSourcePreparer
 if getenv==nil{return runner,false,errors.New("metadata source environment unavailable")}
 switch getenv("PHOTO_DELIVERY_METADATA_SOURCE_ENABLED"){
 case "","false":return runner,false,nil
 case "true":
 default:return runner,false,errors.New("metadata source flag invalid")
 }
 runner.Python=getenv("PHOTO_DELIVERY_METADATA_SOURCE_PYTHON")
 runner.WorkerRoot=getenv("PHOTO_DELIVERY_METADATA_SOURCE_WORKER_ROOT")
 seconds:=int64(600)
 if raw:=getenv("PHOTO_DELIVERY_METADATA_SOURCE_TIMEOUT_SECONDS");raw!=""{
  if len(raw)>3||strings.Trim(raw,"0123456789")!=""{return runner,false,errors.New("metadata source timeout invalid")}
  n,err:=strconv.ParseInt(raw,10,64);if err!=nil||n<1||n>600{return runner,false,errors.New("metadata source timeout invalid")};seconds=n
 }
 runner.Timeout=time.Duration(seconds)*time.Second;runner.Environment=map[string]string{}
 for _,key:=range metadataSourceEnvironmentKeys{if value:=getenv(key);value!=""{runner.Environment[key]=value}}
 // A dedicated API extraction DSN overrides the general service DSN and is
 // mapped to the subprocess allowlist; the parent-only key is never inherited.
 if dsn:=getenv("PHOTO_DELIVERY_METADATA_SOURCE_DATABASE_URL");dsn!=""{runner.Environment["DATABASE_URL"]=dsn}
 if _,err:=runner.environment();err!=nil{return PythonMetadataSourcePreparer{},false,errors.New("metadata source installation configuration invalid")}
 return runner,true,nil
}

// NewMetadataSourceDependencies installs API-side source preparation for one
// tenant. This validates installation only; every call repeats live ACL checks.
func NewMetadataSourceDependencies(ctx context.Context,pool *pgxpool.Pool,tenant uuid.UUID,bucket string,
 objects CreatePreparationObjects,runner PythonMetadataSourcePreparer)(MetadataSourceAPIDependencies,error){
 var zero MetadataSourceAPIDependencies
 if err:=ctx.Err();err!=nil{return zero,err}
 if pool==nil||objects==nil||tenant==uuid.Nil||runner.Environment["S3_BUCKET"]!=bucket{return zero,errors.New("metadata source dependencies invalid")}
 if _,err:=runner.environment();err!=nil{return zero,err}
 original,err:=NewMetadataOriginalResolver(pool,tenant,bucket,objects);if err!=nil{return zero,err}
 query,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 signatures:=[]string{
 "photo.pin_v30_metadata_inventory(uuid,uuid,uuid,uuid,text,text,text,text,text)",
 "photo.read_v30_metadata_source_retry(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text,text[])",
 "photo.v30_delivery_metadata_original(uuid,uuid,uuid,uuid,text,uuid,uuid)",
 "photo.v30_metadata_source_authorized(uuid,uuid,uuid,uuid,jsonb,text,text,jsonb,text,text,text[])",
 "photo.reserve_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text)",
 "photo.complete_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text,uuid,text)",
 }
 var installed bool
 err=pool.QueryRow(query,`SELECT current_user='photo_api' AND coalesce(bool_and(coalesce(p.prosecdef AND owner.rolname='photo_fn_owner' AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 AND NOT EXISTS(SELECT FROM pg_roles forbidden WHERE forbidden.rolname IN('photo_scheduler','photo_worker','photo_gateway','photo_portal') AND has_function_privilege(forbidden.oid,p.oid,'EXECUTE')),false)),false)
 FROM unnest($1::text[]) required(signature)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(signature)
 LEFT JOIN pg_roles owner ON owner.oid=p.proowner`,signatures).Scan(&installed)
 if err!=nil||!installed{return zero,errors.New("metadata source SQL installation unavailable")}
 owned:=map[string]string{};for key,value:=range runner.Environment{owned[key]=value};runner.Environment=owned
 return MetadataSourceAPIDependencies{Original:original,Preparer:runner,Objects:objects,Bucket:bucket},ctx.Err()
}
