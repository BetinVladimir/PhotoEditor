package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

type NegativeReplacementWaveService interface{
 AdvanceProjectReplacementWave(context.Context,auth.Identity,uuid.UUID,uuid.UUID,NegativeDeclinedDecision,int)(NegativeReplacementWavePass,error)
}
type NegativeReplacementIdentityResolver interface{
 ResolveNegativeReplacementIdentity(context.Context,NegativeSchedulerDecision)(auth.Identity,error)
}
type NegativeReplacementScheduler struct{
 Base NegativeSchedulerBackend
 Projects NegativeReplacementWaveService
 Identities NegativeReplacementIdentityResolver
 Limit int
}
func(s NegativeReplacementScheduler)Next(ctx context.Context,after uuid.UUID)(*NegativeSchedulerDecision,error){
 if s.Base==nil{return nil,v2input.ErrInvalid};return s.Base.Next(ctx,after)
}
func(s NegativeReplacementScheduler)Reconcile(ctx context.Context,d NegativeSchedulerDecision)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.Base==nil||s.Projects==nil||s.Identities==nil||s.Limit<1||s.Limit>100{return v2input.ErrInvalid}
 if err:=s.Base.Reconcile(ctx,d);err!=nil{return err}
 if d.Decision.Route=="skip"{return nil}
 if d.Decision.Route!="base"{return v2input.ErrInvalid}
 who,err:=s.Identities.ResolveNegativeReplacementIdentity(ctx,d);if err!=nil{return err}
 if who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return v2input.ErrExpired}
 pass,err:=s.Projects.AdvanceProjectReplacementWave(ctx,who,d.Tenant,d.Project,d.Decision,s.Limit);if err!=nil{return err}
 if len(pass.Deferred)!=0{return v2qualification.ErrUnqualified}
 return nil
}
func(p *NegativeProjects)AdvanceProjectReplacementWave(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,d NegativeDeclinedDecision,limit int)(NegativeReplacementWavePass,error){
 if err:=ctx.Err();err!=nil{return NegativeReplacementWavePass{},err}
 if p==nil{return NegativeReplacementWavePass{},v2qualification.ErrUnqualified}
 service:=p.services[[2]uuid.UUID{tenant,project}];if service==nil{return NegativeReplacementWavePass{},v2qualification.ErrUnqualified}
 return service.AdvanceReplacementWave(ctx,who,tenant,project,d,limit)
}

type SQLNegativeReplacementIdentityResolver struct{DB NegativeTransactions}
func(s SQLNegativeReplacementIdentityResolver)ResolveNegativeReplacementIdentity(ctx context.Context,d NegativeSchedulerDecision)(auth.Identity,error){
 var zero auth.Identity
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||d.Tenant==uuid.Nil||d.Project==uuid.Nil||d.Decision.Review==uuid.Nil||d.Decision.Route!="base"{return zero,v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=s.DB.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return zero,err};if role!="photo_scheduler"{return zero,v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",d.Tenant.String());err!=nil{return zero,err}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_replacement_identity($1,$2,$3,$4)",d.Tenant,d.Project,d.Decision.Review,d.Decision.DecisionHash).Scan(&raw);err!=nil{return zero,err}
 if len(raw)<2||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){return zero,v2input.ErrExpired};if len(raw)>1024{return zero,v2input.ErrInvalid}
 normal,err:=canonical.Bytes(raw);if err!=nil{return zero,v2input.ErrInvalid}
 var wire struct{Actor uuid.UUID `json:"actor_id"`;Session uuid.UUID `json:"auth_session_id"`}
 if json.Unmarshal(normal,&wire)!=nil||wire.Actor==uuid.Nil||wire.Session==uuid.Nil{return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(wire);if err!=nil||!bytes.Equal(normal,exact){return zero,v2input.ErrInvalid}
 if err=tx.Commit(ctx);err!=nil{return zero,err}
 return auth.Identity{UserID:wire.Actor,SessionID:wire.Session},nil
}
