package v2learning

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func readAcceptedTrainingReceipt(ctx context.Context, db interface{ QueryRow(context.Context,string,...any) pgx.Row }, who auth.Identity, tenantID, profileID uuid.UUID, key string, request TrainCreate) (v2input.JobTicket,bool,error) {
 var ticket v2input.JobTicket
 if err:=ctx.Err();err!=nil{return ticket,false,err}
 run,err:=v2input.RunID(tenantID,who.UserID,key);if err!=nil{return ticket,false,err}
 sha,err:=canonical.Hash(request);if err!=nil{return ticket,false,err}
 var raw json.RawMessage
 if err=db.QueryRow(ctx,`SELECT photo.v23_training_accepted_receipt($1,$2,$3,$4,$5,$6,$7)`,tenantID,who.UserID,who.SessionID,profileID,run,key,sha).Scan(&raw);err!=nil{return ticket,false,err}
 if err=ctx.Err();err!=nil{return ticket,false,err}
 if len(raw)==0||string(raw)=="null"{return ticket,false,nil}
 if err=json.Unmarshal(raw,&ticket);err!=nil{return v2input.JobTicket{},false,ErrInvalidTrainingDraft}
 if ticket.RunID!=run||ticket.WorkflowID!=uuid.NewSHA1(run,[]byte("workflow"))||ticket.State!="queued"{return v2input.JobTicket{},false,ErrInvalidTrainingDraft}
 return ticket,true,nil
}
