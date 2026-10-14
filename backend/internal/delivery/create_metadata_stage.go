package delivery

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
func(s Service)prepareAndPinCreateMetadata(ctx context.Context,who auth.Identity,tid uuid.UUID,intent CreateIntent,
 docs CreateDocuments,items []v2input.SessionSnapshotItem,bucket string,objects CreatePreparationObjects,runner PythonMetadataSourcePreparer)error{
 deps,err:=NewMetadataSourceDependencies(ctx,s.DB,tid,bucket,objects,runner);if err!=nil{return err}
 images:=make([]objectstore.Object,0,len(items))
 for _,item:=range items{
  if err=ctx.Err();err!=nil{return err}
  catalog:=v2draft.SQLArtifactCatalog{DB:s.DB,Bucket:bucket,ProjectID:intent.request.ProjectID,AssetVersionID:item.Binding.AssetVersionID}
  image,lookupErr:=catalog.Lookup(ctx,who,tid,v2draft.LayerRef{Ref:item.WorkingImage,Contract:"WorkingImage",CanvasHash:item.Binding.CanvasHash,ExpectedBinding:&item.Binding})
  if lookupErr!=nil{return lookupErr};images=append(images,image)
 }
 inventory,err:=PrepareMetadataInventory(ctx,s,who,tid,intent.request.ProjectID,intent.identity.Run,docs.Snapshot.Ref,docs.Snapshot.Body,images,
 intent.request.Policy.MetadataPolicy,intent.request.Policy.ApprovedFields,deps)
 if err!=nil{return err}
 return tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return intent.PinMetadataInventory(ctx,tx,who.SessionID,inventory,docs.Snapshot.Body)
 })
}
