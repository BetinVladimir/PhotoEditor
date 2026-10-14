package workflow

import (
 "context"
 "errors"
 "github.com/jackc/pgx/v5"
)

// Called after the selected task's authoritative resource class is known, in
// the same leader transaction as reservation. Signer pool does not exempt GPU.
func capacityHeavyAdmission(ctx context.Context, tx pgx.Tx, pin, resourceClass string) (bool,error) {
 if err:=ctx.Err();err!=nil{return false,err}
 switch resourceClass {
 case "cpu","media","archive","collection","mail":return true,nil
 case "gpu":
 default:return false,errors.New("invalid selected capacity resource class")
 }
 if tx==nil||!capacityPolicyDigest.MatchString(pin){return false,errors.New("GPU capacity admission requires installed policy")}
 var available bool
 if err:=tx.QueryRow(ctx,"SELECT photo.capacity_heavy_admission_available($1)",pin).Scan(&available);err!=nil{return false,err}
 return available,nil
}
