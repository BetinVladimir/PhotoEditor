package v2input

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
)

type LensBatchAdmitter interface { Admit(context.Context,LensAdmissionRequest)(JobTicket,error) }

// LensService binds the immutable selection to the authenticated actor/session.
// Policy and capacity are installed configuration, never client parameters.
type LensService struct {
 Catalog *LensCatalog
 Coordinator LensBatchAdmitter
 PolicySHA string
 Limits LensAdmissionLimits
}

func (s LensService) SubmitProjectIntent(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,key string,intent LensIntent)(JobTicket,error) {
 if err:=ctx.Err();err!=nil { return JobTicket{},err }
 if s.Coordinator==nil || who.UserID==uuid.Nil || who.SessionID==uuid.Nil || tenant==uuid.Nil || project==uuid.Nil || !digestPattern.MatchString(s.PolicySHA) || s.Limits.RAMBytes<1 || s.Limits.ScratchBytes<1 || s.Limits.OutputBytes<1 { return JobTicket{},ErrInvalid }
 raw,err:=canonical.Marshal(intent)
 if err!=nil { return JobTicket{},ErrInvalid }
 frozen,err:=DecodeLensIntent(raw)
 if err!=nil { return JobTicket{},err }
 run,err:=RunID(tenant,who.UserID,key)
 if err!=nil { return JobTicket{},err }
 workflow,err:=WorkflowID(run)
 if err!=nil { return JobTicket{},err }
 ticket,err:=s.Coordinator.Admit(ctx,LensAdmissionRequest{Tenant:tenant,Actor:who.UserID,Project:project,Snapshot:frozen.SnapshotID,Session:who.SessionID,Key:key,PolicySHA:s.PolicySHA,Choices:frozen.ProfileChoices(),Limits:s.Limits})
 if err!=nil { return JobTicket{},err }
 if err=ctx.Err();err!=nil { return JobTicket{},err }
 if ticket.RunID!=run || ticket.WorkflowID!=workflow || ticket.State!="queued" { return JobTicket{},ErrInvalid }
 return ticket,nil
}

func(s LensService) ListProfiles(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,after *uuid.UUID)(LensCatalogPage,error){
 if s.Catalog==nil{return LensCatalogPage{},ErrInvalid};return s.Catalog.List(ctx,who,tenant,project,after)
}
func(s LensService) RegisterProfile(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,ref Ref)(LensCatalogEntry,error){
 if s.Catalog==nil{return LensCatalogEntry{},ErrInvalid};return s.Catalog.Register(ctx,who,tenant,project,ref)
}
