package workflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "regexp"
 "github.com/jackc/pgx/v5"
)

var capacityPolicyDigest=regexp.MustCompile(`^[0-9a-f]{64}$`)

func ReadCapacityQueuePolicy(ctx context.Context,tx pgx.Tx,pin string)(CapacityQueuePolicy,error){
 if err:=ctx.Err();err!=nil{return CapacityQueuePolicy{},err}
 if tx==nil||!capacityPolicyDigest.MatchString(pin){return CapacityQueuePolicy{},errors.New("invalid scheduler policy pin")}
 var body *string
 if err:=tx.QueryRow(ctx,"SELECT photo.capacity_scheduler_policy($1)",pin).Scan(&body);err!=nil{return CapacityQueuePolicy{},err}
 if err:=ctx.Err();err!=nil{return CapacityQueuePolicy{},err}
 if body==nil{return CapacityQueuePolicy{},errors.New("scheduler policy not installed")}
 return decodeCapacityQueuePolicy([]byte(*body),pin)
}

func decodeCapacityQueuePolicy(body []byte,pin string)(CapacityQueuePolicy,error){
 var result CapacityQueuePolicy
 if len(body)<2||len(body)>65536||!capacityPolicyDigest.MatchString(pin){return result,errors.New("invalid scheduler policy bytes")}
 sum:=sha256.Sum256(body);if hex.EncodeToString(sum[:])!=pin{return result,errors.New("scheduler policy digest mismatch")}
 var wire struct{
  SchemaVersion int64 `json:"schema_version"`
  Weights map[string]int64 `json:"queue_weights"`
  Unknown int64 `json:"unknown_cost_seconds"`
  Tenant int64 `json:"tenant_heavy_queued"`
 }
 if err:=json.Unmarshal(body,&wire);err!=nil{return result,errors.New("invalid scheduler policy shape")}
 if wire.SchemaVersion!=1{return result,errors.New("invalid scheduler policy version")}
 result=CapacityQueuePolicy{Weights:wire.Weights,UnknownCostSeconds:wire.Unknown,TenantHeavyQueued:wire.Tenant}
 if err:=result.Validate();err!=nil{return CapacityQueuePolicy{},err}
 return result,nil
}
