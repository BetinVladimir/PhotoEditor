package main

import (
 "photoeditor/backend/internal/database"
 "context"
 "errors"
 "os"
 "strconv"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/httpapi"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// Both backend and CPU worker must receive the same independently pinned lens
// registry. Missing deployment configuration leaves admission unavailable.
func buildLens(ctx context.Context,api *pgxpool.Pool,objects *objectstore.Store,bucket string)(httpapi.LensSubmit,func(),error){
 noop:=func(){}
 path:=os.Getenv("PHOTO_V25_LENS_REGISTRY")
 if path=="" {return nil,noop,nil}
 registry,err:=v2input.LoadLensRegistry(path,os.Getenv("PHOTO_V25_LENS_REGISTRY_SHA256"))
 if err!=nil {return nil,noop,errors.New("lens registry invalid")}
 policy:=os.Getenv("PHOTO_V25_LENS_POLICY_SHA256")
 if _,err=domain.ParseDigest(policy);err!=nil {return nil,noop,errors.New("lens policy unavailable")}
 readLimit:=func(name string)(int64,error){n,e:=strconv.ParseInt(os.Getenv(name),10,64);if e!=nil || n<1 || n>9007199254740991 {return 0,errors.New("lens resource limits invalid")};return n,nil}
 ram,err:=readLimit("PHOTO_V25_LENS_RAM_BYTES");if err!=nil {return nil,noop,err}
 scratch,err:=readLimit("PHOTO_V25_LENS_SCRATCH_BYTES");if err!=nil {return nil,noop,err}
 output,err:=readLimit("PHOTO_V25_LENS_OUTPUT_BYTES");if err!=nil {return nil,noop,err}
 dsn:=os.Getenv("V25_LENS_SCHEDULER_DATABASE_URL");if dsn=="" {return nil,noop,errors.New("lens scheduler unavailable")}
 config,err:=pgxpool.ParseConfig(dsn);if err!=nil {return nil,noop,errors.New("lens scheduler invalid")}
 config.ConnConfig.RuntimeParams["role"]="photo_scheduler"
 database.GuardApplicationRole(config)
 scheduler,err:=pgxpool.NewWithConfig(ctx,config);if err!=nil {return nil,noop,errors.New("lens scheduler unavailable")}
 coordinator,err:=(v2input.LensRuntime{API:api,Scheduler:scheduler,Objects:objects,Bucket:bucket,Registry:registry}).Coordinator(ctx)
 if err!=nil {scheduler.Close();return nil,noop,errors.New("lens runtime unavailable")}
 var installed bool
 err=scheduler.QueryRow(ctx,`SELECT to_regprocedure('photo.begin_v25_lens_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint)') IS NOT NULL AND to_regprocedure('photo.stage_v25_lens_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb)') IS NOT NULL AND to_regprocedure('photo.replay_v25_lens_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text)') IS NOT NULL AND to_regprocedure('photo.list_v25_lens_profiles(uuid,uuid,uuid,uuid,uuid)') IS NOT NULL`).Scan(&installed)
 if err!=nil || !installed {scheduler.Close();return nil,noop,errors.New("lens SQL installation unavailable")}
 catalog:=&v2input.LensCatalog{DB:api,Scheduler:scheduler,Source:v2input.SQLLensProfileSource{DB:api,Objects:objects,Bucket:bucket,RuntimeLockSHA:registry.RuntimeLockSHA,QualificationPins:registry.Pins}}
 return v2input.LensService{Catalog:catalog,Coordinator:coordinator,PolicySHA:policy,Limits:v2input.LensAdmissionLimits{RAMBytes:ram,ScratchBytes:scratch,OutputBytes:output}},scheduler.Close,nil
}
