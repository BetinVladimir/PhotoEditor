package workflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "io"
 "os"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2optical"
)

func multiReplacementConfigStem(operation string)(string,error){
 switch operation {
 case "optical.panorama":return "PHOTO_V29_PANORAMA_CONFIG",nil
 case "optical.focus_stack":return "PHOTO_V29_FOCUS_STACK_CONFIG",nil
 default:return "",errors.New("unsupported multi replacement operation")
 }
}

func decodeMultiSchedulerDeployment(operation string,raw []byte,pin string)(v2optical.MultiDeployment,error){
 var zero v2optical.MultiDeployment
 if _,err:=multiReplacementConfigStem(operation);err!=nil{return zero,err}
 if len(raw)<2||len(raw)>8<<20{return zero,errors.New("multi replacement configuration size invalid")}
 if _,err:=domain.ParseDigest(pin);err!=nil{return zero,errors.New("multi replacement configuration digest unavailable")}
 digest:=sha256.Sum256(raw)
 if hex.EncodeToString(digest[:])!=pin{return zero,errors.New("multi replacement configuration digest mismatch")}
 deployment,err:=v2optical.DecodeMultiDeployment(operation,raw)
 if err!=nil{return zero,errors.New("multi replacement configuration invalid")}
 return deployment,nil
}

func buildMultiReplacementProjects(ctx context.Context,pool *pgxpool.Pool,objects *objectstore.Store,bucket,operation string)(*v2optical.MultiProjects,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 stem,err:=multiReplacementConfigStem(operation);if err!=nil{return nil,err}
 path:=os.Getenv(stem);if path==""{return nil,nil}
 if pool==nil||objects==nil||len(bucket)<3||len(bucket)>63{return nil,errors.New("multi replacement dependencies unavailable")}
 file,err:=os.Open(path);if err!=nil{return nil,errors.New("multi replacement configuration unavailable")};defer file.Close()
 info,err:=file.Stat();if err!=nil||!info.Mode().IsRegular()||info.Size()>8<<20{return nil,errors.New("multi replacement configuration invalid")}
 raw,err:=io.ReadAll(io.LimitReader(file,(8<<20)+1));if err!=nil{return nil,errors.New("multi replacement configuration read failed")}
 deployment,err:=decodeMultiSchedulerDeployment(operation,raw,os.Getenv(stem+"_SHA256"));if err!=nil{return nil,err}
 if err=verifyMultiReplacementInstallation(ctx,pool);err!=nil{return nil,err}
 projects,err:=deployment.Build(pool,objects,bucket);if err!=nil{return nil,errors.New("multi replacement deployment refused")}
 return projects,ctx.Err()
}
