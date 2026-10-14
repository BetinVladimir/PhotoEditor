package delivery

import (
 "context"
 "strconv"
 "time"
 "unicode/utf8"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/pagination"
 "photoeditor/backend/internal/tenant"
)

type PackageSummary struct {
 ID uuid.UUID `json:"id"`
 ProjectID uuid.UUID `json:"project_id"`
 WorkflowID uuid.UUID `json:"workflow_id"`
 Name string `json:"name"`
 State string `json:"state"`
 Revision string `json:"revision"`
 CreatedAt time.Time `json:"created_at"`
}

func validPackageSummary(item PackageSummary, project uuid.UUID, afterAt *time.Time, afterID *uuid.UUID) bool {
 if (afterAt==nil)!=(afterID==nil){return false}
 if item.ID==uuid.Nil||item.ProjectID!=project||item.WorkflowID==uuid.Nil||item.CreatedAt.IsZero(){return false}
 if n:=utf8.RuneCountInString(item.Name);n<1||n>200{return false}
 switch item.State {case "building","review","ready","failed","cancelled":default:return false}
 revision,err:=strconv.ParseInt(item.Revision,10,64);if err!=nil||revision<1||revision>9007199254740991||strconv.FormatInt(revision,10)!=item.Revision{return false}
 if afterAt!=nil&&afterID!=nil{
  if item.CreatedAt.Before(*afterAt)||item.CreatedAt.Equal(*afterAt)&&item.ID.String()<=afterID.String(){return false}
 }
 return true
}

func(s Service) List(ctx context.Context,who auth.Identity,tid,projectID uuid.UUID,limit int,raw string)(pagination.Page[PackageSummary],error){
 out:=pagination.Page[PackageSummary]{Items:[]PackageSummary{}}
 if s.DB==nil{return out,domain.NewError("DELIVERY_UNAVAILABLE",503)}
 if projectID==uuid.Nil||limit<1||limit>100{return out,domain.NewError("VALIDATION_FAILED",422)}
 hash,err:=canonical.Hash(map[string]string{"project_id":projectID.String()});if err!=nil{return out,err}
 scope:=pagination.Cursor{Tenant:tid.String(),Principal:who.UserID.String(),FilterHash:hash,Sort:"delivery:created_at,id"}
 var afterID *uuid.UUID;var afterTime *time.Time
 if raw!=""{
  cursor,e:=s.Cursors.Decode(raw,scope);if e!=nil{return out,domain.NewError("INVALID_CURSOR",400)}
  id,e:=uuid.Parse(cursor.LastID);if e!=nil||id==uuid.Nil{return out,domain.NewError("INVALID_CURSOR",400)}
  instant,e:=time.Parse(time.RFC3339Nano,cursor.LastPosition);if e!=nil{return out,domain.NewError("INVALID_CURSOR",400)}
  afterID=&id;afterTime=&instant
 }
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Viewer,func(tx pgx.Tx,_ domain.Role)error{
  rows,e:=tx.Query(ctx,`SELECT id,project_id,workflow_id,name,state,revision,created_at
 FROM photo.list_v30_delivery_packages($1,$2,$3,$4,$5,$6,$7)`,tid,who.UserID,who.SessionID,projectID,afterTime,afterID,limit+1)
  if e!=nil{return e};defer rows.Close()
  previousAt,previousID:=afterTime,afterID
  for rows.Next(){
   var item PackageSummary;var revision int64
   if e=rows.Scan(&item.ID,&item.ProjectID,&item.WorkflowID,&item.Name,&item.State,&revision,&item.CreatedAt);e!=nil{return e}
   item.Revision=strconv.FormatInt(revision,10)
   if len(out.Items)>=limit+1||!validPackageSummary(item,projectID,previousAt,previousID){return domain.NewError("DELIVERY_DOCUMENT_INVALID",500)}
   out.Items=append(out.Items,item)
   previousAt,previousID=&item.CreatedAt,&item.ID
  }
  return rows.Err()
 })
 if err!=nil{return out,err}
 if len(out.Items)>limit{
  out.Items=out.Items[:limit];last:=out.Items[limit-1]
  scope.LastID=last.ID.String();scope.LastPosition=last.CreatedAt.Format(time.RFC3339Nano)
  cursor,e:=s.Cursors.Encode(scope);if e!=nil{return out,e};out.NextCursor=&cursor
 }
 return out,nil
}
