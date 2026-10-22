package v2render

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type LensTaskStatus struct {Ordinal int `json:"ordinal"`;Version uuid.UUID `json:"asset_version_id"`;Task uuid.UUID `json:"task_id"`;State string `json:"state"`;ResultID *uuid.UUID `json:"result_id"`;Result *v2input.Ref `json:"result"`}
type LensStatusPage struct {Run uuid.UUID `json:"run_id"`;Items []LensTaskStatus `json:"items"`;Next *int `json:"next,omitempty"`}
func(s LensProposalService)Status(ctx context.Context,who auth.Identity,tenantID,project,run uuid.UUID,after int)(LensStatusPage,error){
 zero:=LensStatusPage{}
 if s.DB==nil || who.UserID==uuid.Nil || who.SessionID==uuid.Nil || tenantID==uuid.Nil || project==uuid.Nil || run==uuid.Nil || after< -1 || after>4999{return zero,v2input.ErrInvalid}
 page:=LensStatusPage{Run:run,Items:[]LensTaskStatus{}}
 err:=tenant.WithTenant(ctx,s.DB,who,tenantID,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  rows,err:=tx.Query(ctx,`SELECT ordinal,asset_version_id,task_id,state,result_id,artifact_id,sha256 FROM photo.v25_lens_intent_tasks($1,$2,$3,$4,$5,$6)`,tenantID,project,run,who.UserID,who.SessionID,after);if err!=nil{return err};defer rows.Close()
  seen:=map[uuid.UUID]bool{}
  for rows.Next(){var value LensTaskStatus;var id *uuid.UUID;var sha *string
   if err=rows.Scan(&value.Ordinal,&value.Version,&value.Task,&value.State,&value.ResultID,&id,&sha);err!=nil{return err}
   if len(page.Items)>=101 || value.Ordinal<=after || value.Ordinal>4999 || value.Version==uuid.Nil || value.Task==uuid.Nil || seen[value.Version] || (len(page.Items)>0 && value.Ordinal<=page.Items[len(page.Items)-1].Ordinal){return v2input.ErrInvalid};seen[value.Version]=true
   if (value.ResultID==nil)!=(id==nil) || (id==nil)!=(sha==nil){return v2input.ErrInvalid}
   if id!=nil {if *id==uuid.Nil || *value.ResultID==uuid.Nil || !validSHA256(*sha) || value.State!="succeeded"{return v2input.ErrInvalid};value.Result=&v2input.Ref{ArtifactID:*id,SHA256:*sha}}
   page.Items=append(page.Items,value)
  };return rows.Err()
 })
 if err!=nil{return zero,err};if err=ctx.Err();err!=nil{return zero,err}
 if len(page.Items)==0 && after==-1{return zero,v2input.ErrExpired}
 if len(page.Items)>100 {next:=page.Items[99].Ordinal;page.Next=&next;page.Items=page.Items[:100]}
 return page,nil
}
