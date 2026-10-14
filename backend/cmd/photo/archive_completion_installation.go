package main
import (
 "context"
 "errors"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/interchange"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/workflow"
)
type archiveCleanupSequence struct{Generations,Legacy workflow.ExpansionJob}
func(s archiveCleanupSequence)ProcessOne(ctx context.Context)(bool,error){
 if s.Generations==nil||s.Legacy==nil{return false,errors.New("archive cleanup sequence incomplete")}
 done,err:=s.Generations.ProcessOne(ctx);if err!=nil||done{return done,err}
 return s.Legacy.ProcessOne(ctx)
}
type archiveCompletionFactory struct{}
func(archiveCompletionFactory)BuildArchiveCompletion(ctx context.Context,pool *pgxpool.Pool,storage *objectstore.Store,bucket string)(workflow.ExpansionJob,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if pool==nil||storage==nil||len(bucket)<3||len(bucket)>63{return nil,errors.New("archive completion dependencies missing")}
 for _,signature:=range []string{
  "photo.recover_v30_archive_complete(uuid,uuid,uuid)",
  "photo.start_v30_archive_complete(uuid,uuid,uuid)",
  "photo.finish_v30_archive_complete_writer(uuid,uuid,uuid,uuid,text)",
  "photo.claim_v30_archive_completion()",
  "photo.renew_v30_archive_completion(uuid,uuid,uuid)",
  "photo.release_v30_archive_completion(uuid,uuid,uuid)",
  "photo.fail_v30_archive_completion(uuid,uuid,uuid)",
  "photo.finish_v30_archive_completion(uuid,uuid,uuid,text,text,text,bigint,text)",
 }{
  var installed bool
  if err:=pool.QueryRow(ctx,`SELECT coalesce(has_function_privilege(current_user,to_regprocedure($1),'EXECUTE'),false)`,signature).Scan(&installed);err!=nil{return nil,err}
  if !installed{return nil,errors.New("archive completion migration/role not installed: "+signature)}
 }
 return interchange.ArchiveCompletionProcessor{Leases:interchange.SQLArchiveCompletionLeases{DB:pool,Bucket:bucket},Objects:storage,Writers:interchange.SQLArchiveCompleteWriters{DB:pool,Bucket:bucket}},nil
}
var _ workflow.SchedulerArchiveCompletionFactory=archiveCompletionFactory{}

func(archiveCompletionFactory)BuildArchiveExpiry(ctx context.Context,pool *pgxpool.Pool)(workflow.ExpansionJob,error){
 if err:=ctx.Err();err!=nil{return nil,err};if pool==nil{return nil,errors.New("archive expiry database missing")}
 var installed bool
 if err:=pool.QueryRow(ctx,`SELECT coalesce(has_function_privilege(current_user,to_regprocedure('photo.expire_v30_archive_upload()'),'EXECUTE'),false)`).Scan(&installed);err!=nil{return nil,err}
 if !installed{return nil,errors.New("archive expiry migration/role not installed")}
 return interchange.ArchiveUploadExpiry{DB:pool},nil
}
var _ workflow.SchedulerArchiveExpiryFactory=archiveCompletionFactory{}

func(archiveCompletionFactory)BuildArchiveCleanup(ctx context.Context,pool *pgxpool.Pool,storage *objectstore.Store,bucket string)(workflow.ExpansionJob,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if pool==nil||storage==nil||len(bucket)<3||len(bucket)>63{return nil,errors.New("archive cleanup dependencies missing")}
 for _,signature:=range []string{
  "photo.claim_v30_archive_cleanup()","photo.renew_v30_archive_cleanup(uuid,uuid,uuid)",
  "photo.release_v30_archive_cleanup(uuid,uuid,uuid)","photo.finish_v30_archive_cleanup(uuid,uuid,uuid,text,text)",
  "photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text)","photo.v30_archive_drain_multipart(uuid,uuid,text,text)",
  "photo.claim_v30_archive_generation_cleanup()","photo.v30_archive_generation_cleanup_authorized(uuid,uuid,uuid,uuid,text,text,text)",
  "photo.renew_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid,text,text,text)",
  "photo.release_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid)",
  "photo.finish_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid,text,text,text)",
 }{var installed bool
  if err:=pool.QueryRow(ctx,`SELECT coalesce(has_function_privilege(current_user,to_regprocedure($1),'EXECUTE'),false)`,signature).Scan(&installed);err!=nil{return nil,err}
  if !installed{return nil,errors.New("archive cleanup migration/role not installed: "+signature)}
 }
 var keyInstalled bool
 if err:=pool.QueryRow(ctx,`SELECT to_regprocedure('photo.v30_archive_cleanup_key_current(uuid,uuid,text,text)') IS NOT NULL`).Scan(&keyInstalled);err!=nil{return nil,err}
 if !keyInstalled{return nil,errors.New("archive generation parent cleanup key migration not installed")}
 drain:=interchange.SQLArchiveStorageDrain{DB:pool,Objects:storage,Bucket:bucket}
 return archiveCleanupSequence{
  Generations:interchange.ArchiveGenerationCleanupProcessor{Leases:interchange.SQLArchiveGenerationCleanupLeases{DB:pool,Bucket:bucket},Objects:storage},
  Legacy:interchange.ArchiveCleanupProcessor{Leases:interchange.SQLArchiveCleanupLeases{DB:pool,Bucket:bucket},Objects:storage,Writers:interchange.ArchiveCleanupAuthority{DB:pool,Bucket:bucket,Drain:drain}},
 },nil
}
var _ workflow.SchedulerArchiveCleanupFactory=archiveCompletionFactory{}

func(f archiveCompletionFactory)BuildArchiveRecovery(ctx context.Context,pool *pgxpool.Pool,storage *objectstore.Store,bucket string)(workflow.ExpansionJob,error){
 // Require the normal completion dependencies and all publication privileges.
 if _,err:=f.BuildArchiveCompletion(ctx,pool,storage,bucket);err!=nil{return nil,err}
 for _,signature:=range []string{"photo.claim_v30_archive_completion_recovery()","photo.adopt_v30_archive_complete_writer(uuid,uuid,uuid)"}{var installed bool
  if err:=pool.QueryRow(ctx,`SELECT coalesce(has_function_privilege(current_user,to_regprocedure($1),'EXECUTE'),false)`,signature).Scan(&installed);err!=nil{return nil,err}
  if !installed{return nil,errors.New("archive recovery migration/role not installed: "+signature)}
 }
 return interchange.ArchiveCompletionProcessor{Leases:interchange.SQLArchiveCompletionLeases{DB:pool,Bucket:bucket,Recovery:true},Objects:storage,Writers:interchange.SQLArchiveRecoveryWriters{SQLArchiveCompleteWriters:interchange.SQLArchiveCompleteWriters{DB:pool,Bucket:bucket}},ObserveOnly:true},nil
}
var _ workflow.SchedulerArchiveRecoveryFactory=archiveCompletionFactory{}

func(archiveCompletionFactory)BuildArchiveRevocation(ctx context.Context,pool *pgxpool.Pool,storage *objectstore.Store,bucket string)(workflow.ExpansionJob,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if pool==nil||storage==nil||len(bucket)<3||len(bucket)>63{return nil,errors.New("archive revocation dependencies missing")}
 for _,signature:=range []string{"photo.claim_v30_archive_complete_revocation()","photo.finish_v30_archive_complete_revocation(uuid,uuid,uuid,uuid,text,text,text)"}{var installed bool
  if err:=pool.QueryRow(ctx,`SELECT coalesce(has_function_privilege(current_user,to_regprocedure($1),'EXECUTE'),false)`,signature).Scan(&installed);err!=nil{return nil,err}
  if !installed{return nil,errors.New("archive revocation migration/role not installed: "+signature)}
 }
 return interchange.ArchiveCompleteRevocation{DB:pool,Objects:storage,Bucket:bucket},nil
}
var _ workflow.SchedulerArchiveRevocationFactory=archiveCompletionFactory{}

func(archiveCompletionFactory)BuildArchiveInvalidation(ctx context.Context,pool *pgxpool.Pool)(workflow.ExpansionJob,error){
 if err:=ctx.Err();err!=nil{return nil,err};if pool==nil{return nil,errors.New("archive invalidation database missing")}
 var installed bool
 if err:=pool.QueryRow(ctx,`SELECT coalesce(has_function_privilege(current_user,to_regprocedure('photo.invalidate_v30_archive_authority()'),'EXECUTE'),false)`).Scan(&installed);err!=nil{return nil,err}
 if !installed{return nil,errors.New("archive invalidation migration/role not installed")}
 return interchange.ArchiveAuthorityInvalidation{DB:pool},nil
}
var _ workflow.SchedulerArchiveInvalidationFactory=archiveCompletionFactory{}
