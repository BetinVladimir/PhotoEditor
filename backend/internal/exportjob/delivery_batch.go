package exportjob

import (
 "context"
 "bytes"
 "io"
 "encoding/json"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type DeliveryCreator interface{CreateDelivery(context.Context,auth.Identity,uuid.UUID,delivery.CreateRequest,string)(idempotency.Response,error)}
type GroupTicket struct{Key string `json:"key"`;PackageID uuid.UUID `json:"package_id"`;MetadataPolicy string `json:"metadata_policy"`;Presets []uuid.UUID `json:"presets"`;Ticket *v2input.JobTicket `json:"ticket"`;FailureCode *string `json:"failure_code"`}
type BatchAccepted struct{BatchID uuid.UUID `json:"batch_id"`;ProjectID uuid.UUID `json:"project_id"`;State string `json:"state"`;Groups []GroupTicket `json:"groups"`}

// The durable plan commits before child admission. Child keys are deterministic;
// accepted-but-unacknowledged Delivery creation recovers through its own ledger.
// Rows are append-only: neither a retry nor a concurrent caller can rewrite an
// original preset pin, metadata consent, child request or accepted ticket.
func(s Service)createDeliveryBatch(ctx context.Context,who auth.Identity,tid uuid.UUID,key string,req CreateRequest)(idempotency.Response,error){
 var zero idempotency.Response
 if !idempotency.ValidKey(key)||who.SessionID==uuid.Nil{return zero,domain.NewError("VALIDATION_FAILED",422)}
 requestSHA,err:=canonical.Hash(req);if err!=nil{return zero,err}
 batch:=uuid.NewSHA1(tid,[]byte("export:delivery-batch:"+who.UserID.String()+":"+key))
 var groups []DeliveryGroup;var pid uuid.UUID;var historical *idempotency.Response
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Viewer,func(tx pgx.Tx,role domain.Role)error{
  var pin string;var raw []byte
  var oldSHA string;var oldStatus *int;var oldBody []byte;var oldResource *uuid.UUID
  oldErr:=tx.QueryRow(ctx,`SELECT request_hash,response_status,response_body,resource_id FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='Export.Create' AND key=$3`,tid,who.UserID,key).Scan(&oldSHA,&oldStatus,&oldBody,&oldResource)
  if oldErr==nil {
   if oldSHA!=requestSHA{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   if oldStatus==nil||*oldStatus<200||*oldStatus>=300||oldResource==nil||*oldResource==uuid.Nil{return domain.NewError("EXPORT_RECOVERY_UNAVAILABLE",503)}
   e:=tx.QueryRow(ctx,`SELECT c.project_id FROM photo.runs r JOIN photo.snapshots snap ON snap.tenant_id=r.tenant_id AND snap.id=r.input_snapshot_id JOIN photo.collections c ON c.tenant_id=snap.tenant_id AND c.id=snap.collection_id WHERE r.tenant_id=$1 AND r.id=$2 AND r.created_by=$3 AND r.kind='export_batch'`,tid,*oldResource,who.UserID).Scan(&pid);if e!=nil{return e}
   p,e:=currentExportProject(ctx,tx,who,tid,pid);if e!=nil{return e};if !p.DownloadAllowed{return domain.NewError("DOWNLOAD_FORBIDDEN",403)}
   body,e:=canonical.Bytes(oldBody);if e!=nil{return e};historical=&idempotency.Response{Status:*oldStatus,Body:body,ResourceID:oldResource,Replayed:true};return nil
  };if !errors.Is(oldErr,pgx.ErrNoRows){return oldErr}
  e:=tx.QueryRow(ctx,`SELECT request_sha,project_id,plan FROM photo.export_delivery_batches WHERE tenant_id=$1 AND actor_id=$2 AND request_key=$3`,tid,who.UserID,key).Scan(&pin,&pid,&raw)
  if e==nil{if pin!=requestSHA{return domain.NewError("IDEMPOTENCY_CONFLICT",409)};if json.Unmarshal(raw,&groups)!=nil{return errors.New("invalid export group plan")};_,e=currentExportProject(ctx,tx,who,tid,pid);return e}
  if !errors.Is(e,pgx.ErrNoRows){return e}
  var expires *time.Time
  e=tx.QueryRow(ctx,`SELECT c.project_id,s.expires_at FROM photo.snapshots s JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id WHERE s.tenant_id=$1 AND s.id=$2 AND s.kind='selection' AND s.principal_type='user' AND s.principal_id=$3 AND s.item_count BETWEEN 1 AND 5000 FOR SHARE OF s,c`,tid,req.SnapshotID,who.UserID).Scan(&pid,&expires)
  if errors.Is(e,pgx.ErrNoRows)||e==nil&&expires!=nil&&!time.Now().Before(*expires){return domain.NewError("SNAPSHOT_STALE",409)};if e!=nil{return e}
  p,e:=currentExportProject(ctx,tx,who,tid,pid);if e!=nil{return e};if !p.DownloadAllowed{return domain.NewError("DOWNLOAD_FORBIDDEN",403)}
  pins:=make([]PresetPin,0,len(req.Presets))
  for _,id:=range req.Presets{var raw json.RawMessage;var sha string;e=tx.QueryRow(ctx,`SELECT config,config_hash FROM photo.export_presets WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL FOR SHARE`,tid,id).Scan(&raw,&sha);if errors.Is(e,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if e!=nil{return e};body,e:=canonical.Bytes(raw);if e!=nil{return e};config,e:=exportpreset.DecodeFrozenConfig(body,sha);if e!=nil{return e};pins=append(pins,PresetPin{id,sha,config})}
  groups,e=compileDeliveryGroups(tid,who,batch,pid,req.SnapshotID,pins,req.ApprovedFields);if e!=nil{return e};raw,e=canonical.Marshal(groups);if e!=nil{return e}
  _,e=tx.Exec(ctx,`INSERT INTO photo.export_delivery_batches(tenant_id,id,actor_id,project_id,request_key,request_sha,plan) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,actor_id,request_key) DO NOTHING`,tid,batch,who.UserID,pid,key,requestSHA,raw);if e!=nil{return e}
  var committedSHA string;e=tx.QueryRow(ctx,`SELECT request_sha,plan FROM photo.export_delivery_batches WHERE tenant_id=$1 AND id=$2`,tid,batch).Scan(&committedSHA,&raw);if e!=nil{return e};if committedSHA!=requestSHA{return domain.NewError("IDEMPOTENCY_CONFLICT",409)};if _,e=currentExportProject(ctx,tx,who,tid,pid);e!=nil{return e};return json.Unmarshal(raw,&groups)
 });if cancellation:=ctx.Err();cancellation!=nil{return zero,cancellation};if err!=nil{return zero,err};if historical!=nil{return *historical,nil}
 result:=BatchAccepted{BatchID:batch,ProjectID:pid,State:"queued",Groups:make([]GroupTicket,0,len(groups))}
 for _,group:=range groups{
  var ticket *v2input.JobTicket;var failure *string
  err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Viewer,func(tx pgx.Tx,role domain.Role)error{p,e:=currentExportProject(ctx,tx,who,tid,pid);if e!=nil{return e};if !p.DownloadAllowed{return domain.NewError("DOWNLOAD_FORBIDDEN",403)};var raw []byte;e=tx.QueryRow(ctx,`SELECT ticket,failure_code FROM photo.export_delivery_tickets WHERE tenant_id=$1 AND batch_id=$2 AND group_key=$3`,tid,batch,group.Key).Scan(&raw,&failure);if errors.Is(e,pgx.ErrNoRows){return nil};if e!=nil{return e};if failure!=nil{return nil};ticket=new(v2input.JobTicket);return json.Unmarshal(raw,ticket)});if err!=nil{return zero,err}
  if ticket==nil&&failure==nil{
   intent,e:=delivery.FreezeCreateIntent(tid,who,group.Request,group.Key);if e!=nil{return zero,e}
   accepted,e:=s.DeliveryCreator.CreateDelivery(ctx,who,tid,group.Request,group.Key)
   if e==nil{var candidate v2input.JobTicket;identity:=intent.Identity();if accepted.Status!=202||accepted.ResourceID==nil||*accepted.ResourceID!=identity.Package||!decodeDeliveryTicket(accepted.Body,&candidate)||candidate.RunID!=identity.Run||candidate.WorkflowID!=identity.Workflow||candidate.State!="queued"{return zero,domain.NewError("DELIVERY_UNAVAILABLE",503)}
    raw,e:=canonical.Marshal(candidate);if e!=nil{return zero,e}
    e=tenant.WithTenant(ctx,s.DB,who,tid,domain.Viewer,func(tx pgx.Tx,role domain.Role)error{p,e:=currentExportProject(ctx,tx,who,tid,pid);if e!=nil{return e};if !p.DownloadAllowed{return domain.NewError("DOWNLOAD_FORBIDDEN",403)};_,e=tx.Exec(ctx,`INSERT INTO photo.export_delivery_tickets(tenant_id,batch_id,group_key,ticket) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,tid,batch,group.Key,raw);if e!=nil{return e};_,e=currentExportProject(ctx,tx,who,tid,pid);return e});if e!=nil{return zero,e};ticket=&candidate
   }else{if ctx.Err()!=nil{return zero,ctx.Err()}
    // A returned error code is not proof that child admission did not commit.
    // Keep the frozen group pending; its deterministic key recovers the exact
    // ticket on retry, including errors after an unacknowledged acceptance.
   }
  }
  // The append winner is authoritative even under concurrent exact retries.
  // Never return a locally accepted/refused outcome that lost the row race.
  err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Viewer,func(tx pgx.Tx,role domain.Role)error{p,x:=currentExportProject(ctx,tx,who,tid,pid);if x!=nil{return x};if !p.DownloadAllowed{return domain.NewError("DOWNLOAD_FORBIDDEN",403)};var raw []byte;var storedFailure *string;x=tx.QueryRow(ctx,`SELECT ticket,failure_code FROM photo.export_delivery_tickets WHERE tenant_id=$1 AND batch_id=$2 AND group_key=$3`,tid,batch,group.Key).Scan(&raw,&storedFailure);if errors.Is(x,pgx.ErrNoRows){ticket=nil;failure=nil;return nil};if x!=nil{return x};failure=storedFailure;ticket=nil;if failure==nil{ticket=new(v2input.JobTicket);if !decodeDeliveryTicket(raw,ticket){return errors.New("invalid export ticket")}};return nil});if err!=nil{return zero,err}
  identity,e:=delivery.FreezeCreateIntent(tid,who,group.Request,group.Key);if e!=nil{return zero,e}
  result.Groups=append(result.Groups,GroupTicket{group.Key,identity.Identity().Package,group.Request.Policy.MetadataPolicy,append([]uuid.UUID{},group.Request.Policy.RequiredPresets...),ticket,failure})
 }
 pending,failed:=false,false;for _,group:=range result.Groups{if group.FailureCode!=nil{failed=true}else if group.Ticket==nil{pending=true}}
 if pending{result.State="partial"}else if failed{result.State="failed"}
 if err:=ctx.Err();err!=nil{return zero,err}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Viewer,func(tx pgx.Tx,_ domain.Role)error{_,e:=currentExportProject(ctx,tx,who,tid,pid);return e});if err!=nil{return zero,err}
 body,err:=canonical.Marshal(result);return idempotency.Response{Status:202,Body:body,ResourceID:&batch},err
}

type BatchGroupStatus struct{GroupTicket;RunStatus string `json:"run_status"`}
type BatchStatus struct{BatchID uuid.UUID `json:"batch_id"`;ProjectID uuid.UUID `json:"project_id"`;State string `json:"state"`;Groups []BatchGroupStatus `json:"groups"`}
// Read projects the actual accepted child runs; queued acceptance receipts are
// deliberately not treated as completion evidence.
func(s Service)GetDeliveryBatch(ctx context.Context,who auth.Identity,tid,id uuid.UUID)(BatchStatus,error){
 out:=BatchStatus{BatchID:id,State:"pending",Groups:[]BatchGroupStatus{}}
 if s.DB==nil||tid==uuid.Nil||id==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Viewer,func(tx pgx.Tx,role domain.Role)error{
  var pid uuid.UUID;var raw []byte;e:=tx.QueryRow(ctx,`SELECT project_id,plan FROM photo.export_delivery_batches WHERE tenant_id=$1 AND id=$2 AND actor_id=$3`,tid,id,who.UserID).Scan(&pid,&raw);if errors.Is(e,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if e!=nil{return e}
  p,e:=currentExportProject(ctx,tx,who,tid,pid);if e!=nil{return e};if !p.DownloadAllowed{return domain.NewError("DOWNLOAD_FORBIDDEN",403)}
  out.ProjectID=pid
  var groups []DeliveryGroup;if json.Unmarshal(raw,&groups)!=nil{return errors.New("invalid export group plan")}
  complete,failed,pending:=0,0,0
  for _,group:=range groups{
   intent,e:=delivery.FreezeCreateIntent(tid,who,group.Request,group.Key);if e!=nil{return e};var ticketRaw []byte;var status *string;var failure *string
   e=tx.QueryRow(ctx,`SELECT t.ticket,r.status,t.failure_code FROM photo.export_delivery_tickets t LEFT JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=(t.ticket->>'run_id')::uuid WHERE t.tenant_id=$1 AND t.batch_id=$2 AND t.group_key=$3`,tid,id,group.Key).Scan(&ticketRaw,&status,&failure)
   item:=BatchGroupStatus{GroupTicket:GroupTicket{group.Key,intent.Identity().Package,group.Request.Policy.MetadataPolicy,append([]uuid.UUID{},group.Request.Policy.RequiredPresets...),nil,nil},RunStatus:"pending"}
   if errors.Is(e,pgx.ErrNoRows){pending++}else{if e!=nil{return e};if failure!=nil{item.FailureCode=failure;item.RunStatus="failed";failed++;out.Groups=append(out.Groups,item);continue};var ticket v2input.JobTicket;if json.Unmarshal(ticketRaw,&ticket)!=nil{return errors.New("invalid export ticket")};item.Ticket=&ticket;if status==nil{item.RunStatus="unavailable";failed++}else{item.RunStatus=*status;switch deliveryBatchChildOutcome(*status){case "complete":complete++;case "failed":failed++;default:pending++}}}
   out.Groups=append(out.Groups,item)
  }
  switch{case complete==len(groups):out.State="completed";case failed>0&&pending==0:out.State="failed";case failed>0:out.State="partial";default:out.State="running"};_,e=currentExportProject(ctx,tx,who,tid,pid);return e
 });if cancellation:=ctx.Err();cancellation!=nil{return BatchStatus{},cancellation};return out,err
}

func decodeDeliveryTicket(raw []byte,out *v2input.JobTicket)bool{
 if len(raw)<2||len(raw)>4096{return false};decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields();return decoder.Decode(out)==nil&&decoder.Decode(new(any))==io.EOF
}

// Required-preset batches cannot report success from partial child completion.
// Unknown or in-progress states remain pending until current authority is read.
func deliveryBatchChildOutcome(status string) string {
 switch status {
 case "completed": return "complete"
 case "failed", "cancelled", "completed_with_errors": return "failed"
 default: return "pending"
 }
}
