package interchange

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// This local authority binds to the original accepted image BEFORE import UUID
// remapping. It deliberately cannot authorize a foreign archive producer.
// Detector qualification, consent and physical arrays remain independent gates.
type SQLImportHistoricalImageAuthority struct {
 DB *pgxpool.Pool
 Tenant,Project,Entry uuid.UUID
 Old,Target v2input.SessionVersionBinding
 Sources ImportRecipeSourceQualifier
}
func(q SQLImportHistoricalImageAuthority)QualifyImportedHistoricalImage(ctx context.Context,entry uuid.UUID,original v2input.Ref,originalBody,targetBody []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.DB==nil||q.Sources==nil||q.Tenant==uuid.Nil||q.Project==uuid.Nil||entry!=q.Entry||entry==uuid.Nil||!validImportRef(original)||rawSHA(originalBody)!=original.SHA256||rawSHA(targetBody)!=q.Target.Source.SHA256{return v2input.ErrInvalid}
 if err:=validateImportHistoricalImageGeometry(originalBody,targetBody);err!=nil{return err}
 check:=func()error{
  if err:=q.Sources.QualifyImportRecipeSource(ctx,entry,q.Old,q.Target);err!=nil{return err}
  return database.TenantTransaction(ctx,q.DB,q.Tenant,func(tx pgx.Tx)error{
   var allowed bool
   if err:=tx.QueryRow(ctx,`SELECT photo.v30_canonical_image_ancestor($1,$2,$3,$4,$5,$6)`,q.Tenant,q.Project,q.Old.Source.ArtifactID,q.Old.Source.SHA256,original.ArtifactID,original.SHA256).Scan(&allowed);err!=nil{return err}
   if !allowed{return v2input.ErrInvalid};return ctx.Err()
  })
 }
 if err:=check();err!=nil{return err}
 return check()
}
func validateImportHistoricalImageGeometry(originalBody,targetBody []byte)error{
 original,err:=review.ParseWorkingImage(originalBody);if err!=nil{return err}
 target,err:=review.ParseWorkingImage(targetBody);if err!=nil{return err}
 if original.Canvas!=target.Canvas||original.Color!=target.Color||original.DType!=target.DType||!bytes.Equal(original.TransformChain,target.TransformChain){return v2input.ErrInvalid}
 return nil
}
