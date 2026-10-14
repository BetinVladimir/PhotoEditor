package workflow
import (
 "context"
 "github.com/jackc/pgx/v5"
)
// WasAdopted acknowledges only an exact immutable adoption receipt with matching
// task/inbox/event evidence. It performs no source IO or adoption mutation and
// does not authorize applying a downstream replacement perspective result.
func(r ReplacementDetectionDocuments) WasAdopted(ctx context.Context)(bool,error){
 scope,_,_,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return false,err}
 confirmed:=false
 err=r.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  return tx.QueryRow(ctx,"SELECT photo.v29_replacement_detector_adoption_committed($1,$2,$3,$4)",scope.Tenant,scope.Owner,string(r.Plan.Descriptor()),r.Snapshot).Scan(&confirmed)
 });if err!=nil{return false,err};return confirmed,nil
}
