package workflow

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
const watermarkStageResultQuery=`SELECT photo.v30_watermark_stage_result($1,$2,$3,$4)`
// ReadWatermarkStageResult discovers producer metadata only. Current pin/lease
// authority is checked by SQL; physical verification and adoption still follow.
func ReadWatermarkStageResult(ctx context.Context,db artifactjson.RowQuerier,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,bucket string)(*DeliveryStageResult,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if db==nil||lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||lease.Run==uuid.Nil||request.ParentRunID!=lease.Run||request.ChildRunID==uuid.Nil||request.SchemaVersion!=1||request.SlotIndex<0||request.SlotIndex>=80000||len(bucket)<3||len(bucket)>63{return nil,v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return nil,err}
 task,err:=v2input.TaskID(workflow,"derivative.watermark",0);if err!=nil{return nil,err}
 var raw []byte
 if err=db.QueryRow(ctx,watermarkStageResultQuery,lease.Expansion,lease.Token,request.SlotIndex,"derivative.watermark").Scan(&raw);err!=nil{return nil,err}
 if err=ctx.Err();err!=nil{return nil,err}
 if len(raw)==0||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){return nil,nil}
 return parseDeliveryStageResult(raw,task,bucket)
}
