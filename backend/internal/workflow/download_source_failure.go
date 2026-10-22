package workflow

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/v2input"
)

// A bounded observation of one exact frozen master. Ordinary transport errors
// remain retryable; only proven missing/corrupt bytes or a closed contract failure
// may terminalize the accepted job. SQL repeats current phase and source pins.
type downloadSourceFailure struct{ordinal int64;master uuid.UUID;sha,code string}
func(e *downloadSourceFailure)Error()string{return "download source unavailable: "+e.code}
func downloadPhysicalSourceFailure(master pendingDownloadMaster,err error)error{
 code:="";if errors.Is(err,objectstore.ErrMissing){code="ARTIFACT_MISSING"}else if errors.Is(err,objectstore.ErrIntegrity){code="ARTIFACT_HASH_MISMATCH"}
 if errors.Is(err,masknpy.ErrInvalid)||errors.Is(err,v2input.ErrInvalid){code="SOURCE_INPUT_INVALID"};if code==""{return err};return &downloadSourceFailure{master.ordinal,master.object.ID,master.object.SHA,code}
}
func(r DownloadExportPlanRepository)FailPlanningSource(ctx context.Context,tenant,run uuid.UUID,observation *downloadSourceFailure)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err};if r.DB==nil||tenant==uuid.Nil||run==uuid.Nil{return false,errors.New("invalid download source failure repository")}
 var ordinal *int64;var master *uuid.UUID;var sha,code *string
 if observation!=nil{ordinal=&observation.ordinal;master=&observation.master;sha=&observation.sha;code=&observation.code}
 var changed bool
 err:=r.DB.QueryRow(ctx,"SELECT photo.fail_download_planning_source($1,$2,$3,$4,$5,$6)",tenant,run,ordinal,master,sha,code).Scan(&changed)
 return changed,err
}
