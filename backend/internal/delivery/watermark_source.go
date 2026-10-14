package delivery

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type WatermarkSource struct {
 Asset uuid.UUID
 Version uuid.UUID
 Ref v2input.Ref
 Object objectstore.Object
}
// ResolveWatermarkSource requires an explicit frozen version, never latest.
// Catalog repeats provenance and live ACL after the binding transaction ends.
// Returned object still requires physical WorkingImage/array verification.
func(s Service)ResolveWatermarkSource(ctx context.Context,who auth.Identity,tid,project,asset,version uuid.UUID,bucket string)(WatermarkSource,error){
 var out WatermarkSource
 if err:=ctx.Err();err!=nil{return out,err}
 if tid==uuid.Nil||project==uuid.Nil||asset==uuid.Nil||version==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||len(bucket)<3||len(bucket)>63{return out,v2input.ErrInvalid}
 if s.DB==nil{return out,domain.NewError("DELIVERY_UNAVAILABLE",503)}
 out.Asset=asset
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,`SELECT asset_version_id,artifact_id,sha256 FROM photo.v30_delivery_watermark_source($1,$2,$3,$4,$5,$6)`,tid,project,asset,version,who.UserID,who.SessionID).Scan(&out.Version,&out.Ref.ArtifactID,&out.Ref.SHA256)
 })
 if errors.Is(err,pgx.ErrNoRows){return WatermarkSource{},v2input.ErrExpired};if err!=nil{return WatermarkSource{},err}
 if out.Version!=version||out.Ref.ArtifactID==uuid.Nil||!shaPattern.MatchString(out.Ref.SHA256){return WatermarkSource{},v2input.ErrInvalid}
 out.Object,err=(v2draft.SQLArtifactCatalog{DB:s.DB,Bucket:bucket,ProjectID:project,AssetVersionID:version}).Lookup(ctx,who,tid,v2draft.LayerRef{Ref:out.Ref,Contract:"WorkingImage"})
 if err!=nil{return WatermarkSource{},err};return out,ctx.Err()
}
