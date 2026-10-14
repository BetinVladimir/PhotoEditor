package v2optical

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// LockUnchangedSource retains exact original consumer/source/array authority in
// the caller transaction. No IO or commit; roll back every refusal. Replacement
// task membership and fresh qualification/admission remain independent gates.
func(p PreparedNegativeReplacementInputs)LockUnchangedSource(ctx context.Context,tx pgx.Tx,proof VerifiedNegativeUnchangedArrays)error{
 if err:=ctx.Err();err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 checked,_,_,err:=p.checked(ctx);if err!=nil{return err}
 source:=proof.selection;l:=source.step.intent.locator
 if proof.who.UserID!=checked.who.UserID||proof.who.SessionID!=checked.who.SessionID||source.step.intent.who.UserID!=checked.who.UserID||source.step.intent.who.SessionID!=checked.who.SessionID||l.Tenant!=checked.tenant||l.Project!=checked.project||l.Review!=checked.review||l.Hash!=checked.hash||source.locator.Bucket!=checked.bucket{return v2input.ErrInvalid}
 found:=false;for _,intent:=range checked.intents{if intent.locator==l&&intent.raw==source.step.intent.raw{found=true}};if !found{return v2input.ErrInvalid}
 specs,err:=unchangedSourceArraySpecs(source);if err!=nil{return err}
 if len(specs)==0||len(specs)!=len(proof.specs)||len(specs)!=len(proof.objects){return v2input.ErrInvalid}
 pins:=make([]negativeSelectedArrayPin,0,len(specs))
 for i,spec:=range specs{
  if spec!=proof.specs[i]{return v2input.ErrInvalid};object:=proof.objects[i]
  locator:=negativeSourceLocator{ArtifactID:spec.Ref.ArtifactID,SHA256:object.SHA256,Bucket:checked.bucket,Key:object.Key,Version:object.VersionID,Bytes:object.Bytes,Media:object.ContentType}
  raw,err:=canonical.Marshal(locator);if err!=nil{return err};verified,err:=decodeNegativeSourceArrayObject(raw,checked.bucket,spec);if err!=nil||verified!=object{return v2input.ErrInvalid}
  pins=append(pins,negativeSelectedArrayPin{Role:spec.Role,Object:locator})
 }
 metadata,err:=canonical.Marshal(source.locator);if err!=nil{return err};packet,err:=canonical.Marshal(pins);if err!=nil{return err}
 var role string;if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return err};if role!="photo_scheduler"{return v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",checked.tenant.String());err!=nil{return err}
 var allowed bool
 if err=tx.QueryRow(ctx,"SELECT photo.lock_v29_negative_unchanged_proof($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12::jsonb,$13::jsonb)",checked.tenant,checked.project,checked.who.UserID,checked.who.SessionID,checked.review,checked.hash,checked.planJCS,l.Task,source.binding.Source.ArtifactID,source.binding.Source.SHA256,source.raw,string(metadata),string(packet)).Scan(&allowed);err!=nil{return err}
 if !allowed{return v2input.ErrExpired};return ctx.Err()
}
