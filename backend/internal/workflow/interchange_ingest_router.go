package workflow
import(
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/database"
)
// IngestCatalogRouter chooses the source-specific catalog commit on current SQL
// evidence. A denied/missing imported principal never falls back to upload.
type IngestCatalogRouter struct{DB *pgxpool.Pool;Legacy IngestDecodeAdvance;Source IngestCatalogSource;WorkICCSHA string}
func(r IngestCatalogRouter)Advance(ctx context.Context,tenant uuid.UUID,spec IngestSpec)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if r.DB==nil||r.Legacy==nil||r.Source==nil||tenant==uuid.Nil||spec.RunID==uuid.Nil{return false,errors.New("invalid ingest catalog router")}
 var imported bool;var actor,session *uuid.UUID
 err:=database.TenantTransaction(ctx,r.DB,tenant,func(tx pgx.Tx)error{return tx.QueryRow(ctx,`SELECT imported,actor_id,session_id FROM photo.v30_ingest_catalog_principal($1,$2)`,tenant,spec.RunID).Scan(&imported,&actor,&session)})
 if err!=nil{return false,err}
 if !imported{return r.Legacy.Advance(ctx,tenant,spec)}
 if actor==nil||session==nil||*actor==uuid.Nil||*session==uuid.Nil{return false,errors.New("interchange ingest principal unavailable")}
 writer:=scopedImportedCatalog{DB:r.DB,Who:auth.Identity{UserID:*actor,SessionID:*session},WorkICCSHA:r.WorkICCSHA}
 return (IngestCatalogCoordinator{Source:r.Source,Catalog:writer}).advance(ctx,tenant,spec,false)
}
type scopedImportedCatalog struct{DB *pgxpool.Pool;Who auth.Identity;WorkICCSHA string}
func(r scopedImportedCatalog)AdoptImage(ctx context.Context,tenant,run uuid.UUID,image CatalogImage)(bool,error){
 var accepted bool
 err:=database.TenantTransaction(ctx,r.DB,tenant,func(tx pgx.Tx)error{var err error;accepted,err=(ImportedCatalogAdopter{DB:tx,Who:r.Who,WorkICCSHA:r.WorkICCSHA}).AdoptImage(ctx,tenant,run,image);return err})
 return accepted,err
}
func(r scopedImportedCatalog)RejectFile(ctx context.Context,tenant,run uuid.UUID,f IngestItemFailure)(bool,error){
 var accepted bool
 err:=database.TenantTransaction(ctx,r.DB,tenant,func(tx pgx.Tx)error{var err error;accepted,err=(ImportedCatalogAdopter{DB:tx,Who:r.Who,WorkICCSHA:r.WorkICCSHA}).RejectFile(ctx,tenant,run,f);return err})
 return accepted,err
}
func(r scopedImportedCatalog)Finish(ctx context.Context,tenant,run uuid.UUID)(string,error){
 var state string
 err:=database.TenantTransaction(ctx,r.DB,tenant,func(tx pgx.Tx)error{var err error;state,err=(ImportedCatalogAdopter{DB:tx,Who:r.Who,WorkICCSHA:r.WorkICCSHA}).Finish(ctx,tenant,run);return err})
 return state,err
}
