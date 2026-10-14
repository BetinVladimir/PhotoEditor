package v2optical

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// lockNegativeSourceProof performs no object I/O and never commits. The caller
// retains every FOR SHARE lock until graph sealing and its receipt commit.
func lockNegativeSourceProof(ctx context.Context, tx pgx.Tx, bucket string, who auth.Identity, tenant,project,snapshot uuid.UUID, params NegativeParams, sourceRaw []byte, proof *negativeSourceProof) error {
 if err:=ctx.Err();err!=nil { return err }
 if tx==nil || proof==nil || bucket=="" || len(bucket)>255 || tenant==uuid.Nil || project==uuid.Nil || snapshot==uuid.Nil || who.UserID==uuid.Nil || who.SessionID==uuid.Nil { return v2input.ErrInvalid }
 if proof.who.UserID!=who.UserID || proof.who.SessionID!=who.SessionID || proof.tenant!=tenant || proof.project!=project || proof.snapshot!=snapshot || proof.params!=params || proof.source!=string(sourceRaw) { return v2input.ErrInvalid }
 specs,err:=NegativeSourceArrays(params,sourceRaw);if err!=nil { return err }
 if len(specs)==0 || len(specs)!=len(proof.specs) || len(specs)!=len(proof.objects) { return v2input.ErrInvalid }
 for i,spec:=range specs { if spec!=proof.specs[i] { return v2input.ErrInvalid } }
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil { return err }
 if role!="photo_scheduler" { return v2input.ErrInvalid }
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil { return err }
 base,err:=canonical.Marshal(params.Base);if err!=nil { return err }
 for i,spec:=range specs {
  if err:=ctx.Err();err!=nil { return err }
  var raw []byte
  if err:=tx.QueryRow(ctx,"SELECT photo.v29_negative_array_source($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)",tenant,project,snapshot,who.UserID,who.SessionID,string(base),string(sourceRaw),spec.Ref.ArtifactID,spec.Ref.SHA256,spec.Role).Scan(&raw);err!=nil { return err }
  object,err:=decodeNegativeSourceArrayObject(raw,bucket,spec);if err!=nil { return err }
  if object!=proof.objects[i] { return v2input.ErrExpired }
 }
 return ctx.Err()
}
