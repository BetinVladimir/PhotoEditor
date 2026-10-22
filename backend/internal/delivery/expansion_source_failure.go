package delivery
import("context";"github.com/google/uuid";"photoeditor/backend/internal/domain";"photoeditor/backend/internal/v2input")
func(s ExpansionLeaseStore)FailPhysicalSource(ctx context.Context,l ExpansionLease,ref v2input.Ref,version,code string,slot int,request string,watermark bool)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err};if s.DB==nil||!l.Valid()||s.Instance==uuid.Nil||len(version)<1||version=="null"{return false,domain.NewError("DELIVERY_LEASE_INVALID",422)}
 var accepted bool;err:=s.DB.QueryRow(ctx,`SELECT photo.fail_v30_delivery_physical_source($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,s.Instance,s.Bucket,l.Expansion,l.Token,ref.ArtifactID,ref.SHA256,version,code,slot,request,watermark).Scan(&accepted);if cancellation:=ctx.Err();cancellation!=nil{return false,cancellation};return accepted,err
}
