package interchange

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type ExportStyleBackgroundAuthority interface{QualifyExportStyleBackground(context.Context,v2draft.FrozenBackgroundAsset)error}
type SQLExportStyleBackgroundAuthority struct{
 Resolver v2draft.SQLBackgroundAssetResolver
 Tenant,Project uuid.UUID
 Who auth.Identity
 Native ExportBinaryQualifier
}
func(s SQLExportStyleBackgroundAuthority)QualifyExportStyleBackground(ctx context.Context,pin v2draft.FrozenBackgroundAsset)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.Native==nil{return v2input.ErrInvalid}
 asset,err:=s.Resolver.Resolve(ctx,s.Who,s.Tenant,s.Project,pin.Style,pin.PortableAssetID);if err!=nil{return err}
 if asset.PortableID!=pin.PortableAssetID||asset.StyleVersionID!=pin.StyleVersionID||asset.Ref!=pin.Source||asset.Object.Bytes!=pin.ExpectedBytes||asset.Object.ContentType!=pin.MediaType{return v2input.ErrInvalid}
 if err:=s.Native.QualifyExportBinary(ctx,v2input.Entry{Ref:asset.Ref,Contract:"EncodedImage"},asset.Object);err!=nil{return err}
 current,err:=s.Resolver.Resolve(ctx,s.Who,s.Tenant,s.Project,pin.Style,pin.PortableAssetID);if err!=nil{return err}
 if current.PortableID!=asset.PortableID||current.StyleVersionID!=asset.StyleVersionID||current.Ref!=asset.Ref||current.Object!=asset.Object{return v2input.ErrInvalid};return ctx.Err()
}
func qualifyExportStyleBackground(ctx context.Context,raw []byte,style v2input.Ref,recipe v2draft.FrozenRenderRecipe,authority ExportStyleBackgroundAuthority)error{
 var document struct{Extensions struct{Background *struct{Mode string `json:"mode"`;AssetID *string `json:"asset_id"`} `json:"background"`} `json:"extensions"`}
 if err:=json.Unmarshal(raw,&document);err!=nil{return err}
 background:=document.Extensions.Background
 if background==nil||background.Mode!="asset"{if recipe.BackgroundAsset!=nil{return v2input.ErrInvalid};return nil}
 pin:=recipe.BackgroundAsset
 if background.AssetID==nil||pin==nil||pin.PortableAssetID!=*background.AssetID||pin.Style!=style||pin.WorkICCSHA!=recipe.Scalars.ICCLockSHA||authority==nil{return v2input.ErrInvalid}
 return authority.QualifyExportStyleBackground(ctx,*pin)
}
