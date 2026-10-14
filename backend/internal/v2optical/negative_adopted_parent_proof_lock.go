package v2optical

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func adoptedParentProofPacket(ctx context.Context,bucket string,who auth.Identity,tenant,project uuid.UUID,proof VerifiedAdoptedParentArrays)([]byte,[]byte,error){
 if bucket==""||len(bucket)>255||who.UserID==uuid.Nil||who.SessionID==uuid.Nil {return nil,nil,v2input.ErrInvalid}
 p:=PreparedNegativeReplacementInputs{bucket:bucket,who:who,tenant:tenant,project:project}
 if err:=validAdoptedParentProof(ctx,p,proof);err!=nil {return nil,nil,err}
 s:=proof.selection.metadata
 specs,err:=adoptedParentArraySpecs(proof.selection);if err!=nil {return nil,nil,err}
 if len(specs)==0||len(specs)!=len(proof.specs)||len(specs)!=len(proof.objects) {return nil,nil,v2input.ErrInvalid}
 pins:=make([]negativeSelectedArrayPin,0,len(specs))
 for i,spec:=range specs {
  if spec!=proof.specs[i] {return nil,nil,v2input.ErrInvalid}
  object:=proof.objects[i]
  locator:=negativeSourceLocator{ArtifactID:spec.Ref.ArtifactID,SHA256:object.SHA256,Bucket:bucket,Key:object.Key,Version:object.VersionID,Bytes:object.Bytes,Media:object.ContentType}
  raw,err:=canonical.Marshal(locator);if err!=nil {return nil,nil,err}
  checked,err:=decodeNegativeSourceArrayObject(raw,bucket,spec);if err!=nil||checked!=object {return nil,nil,v2input.ErrInvalid}
  pins=append(pins,negativeSelectedArrayPin{Role:spec.Role,Object:locator})
 }
 selection,err:=canonical.Marshal(s);if err!=nil {return nil,nil,err};body,err:=canonical.Marshal(pins);if err!=nil {return nil,nil,err}
 return selection,body,nil
}

// LockAdoptedParentArrays performs no IO/commit. Caller must roll back on
// every refusal and retain this transaction through replacement admission. The
// whole graph's run/task lock ordering remains the admission owner's concern.
func LockAdoptedParentArrays(ctx context.Context,tx pgx.Tx,bucket string,who auth.Identity,tenant,project uuid.UUID,proof VerifiedAdoptedParentArrays)error{
 if err:=ctx.Err();err!=nil {return err};if tx==nil {return v2input.ErrInvalid}
 metadata,pins,err:=adoptedParentProofPacket(ctx,bucket,who,tenant,project,proof);if err!=nil {return err}
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil {return err};if role!="photo_scheduler" {return v2input.ErrInvalid}
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil {return err}
 s:=proof.selection.metadata
 var allowed bool
 if err:=tx.QueryRow(ctx,"SELECT photo.lock_v29_adopted_parent_proof($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb)",tenant,project,who.UserID,who.SessionID,s.Task,s.Run,s.Result,proof.selection.raw,string(metadata),string(pins)).Scan(&allowed);err!=nil {return err}
 if !allowed {return v2input.ErrExpired};return ctx.Err()
}
