package delivery

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// PinWatermarkSource runs in the caller's authenticated API tenant transaction.
// It performs no object IO or commit; SQL repeats frozen request/source authority.
func(i CreateIntent)PinWatermarkSource(ctx context.Context,db ProofTransaction,currentSession uuid.UUID,pin FrozenWatermarkSource)error{
 if err:=ctx.Err();err!=nil{return err}
 if db==nil||currentSession==uuid.Nil||i.tenant==uuid.Nil||i.actor==uuid.Nil||i.identity.Run==uuid.Nil||i.key==""||pin.pin==""||len(pin.body)>2<<20||digest(pin.body)!=pin.pin{return v2input.ErrInvalid}
 var envelope watermarkPinEnvelope
 if json.Unmarshal(pin.body,&envelope)!=nil||envelope.SchemaVersion!=1||envelope.Scope.Tenant!=i.tenant||envelope.Scope.Project!=i.request.ProjectID||envelope.Scope.Actor!=i.actor||envelope.Scope.Parent!=i.identity.Run{return v2input.ErrInvalid}
 selected:=false
 for _,id:=range i.request.Policy.RequiredPresets{if id==envelope.Scope.Preset{selected=true}}
 for _,id:=range i.request.Policy.OptionalPresets{if id==envelope.Scope.Preset{selected=true}}
 if !selected{return v2input.ErrInvalid}
 var accepted bool
 err:=db.QueryRow(ctx,`SELECT photo.pin_v30_watermark_source($1,$2,$3,$4,$5,$6,$7,$8)`,i.tenant,i.identity.Run,i.actor,currentSession,i.key,i.requestSHA,string(pin.body),pin.pin).Scan(&accepted)
 if err!=nil{return err};if !accepted{return v2input.ErrExpired};return ctx.Err()
}
