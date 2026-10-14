package maintenance

import (
 "context"
 "errors"
 "github.com/google/uuid"
)

// TenantPurgeBucket binds a storage client to an operator-installed bucket.
// Every installed bucket must be present before TenantCleaner can finish.
type TenantPurgeBucket struct {
 Name string
 Objects TenantDeletionObjects
}

type TenantPurgeBuckets []TenantPurgeBucket

// PurgeTenantBatch divides one physical deletion budget across all buckets.
// A false result proves every bucket completed its own empty verification pass.
func (b TenantPurgeBuckets) PurgeTenantBatch(ctx context.Context, tenant uuid.UUID, limit int) (bool,error) {
 if tenant==uuid.Nil||limit<1||limit>256||len(b)==0||len(b)>limit{return false,errors.New("invalid tenant purge bucket configuration")}
 seen:=make(map[string]bool,len(b))
 for _,bucket:=range b {
  if bucket.Name==""||bucket.Objects==nil||seen[bucket.Name]{return false,errors.New("invalid tenant purge bucket configuration")}
  seen[bucket.Name]=true
 }
 more:=false
 share,remainder:=limit/len(b),limit%len(b)
 for index,bucket:=range b {
  if err:=ctx.Err();err!=nil{return false,err}
  budget:=share;if index<remainder{budget++}
  pending,err:=bucket.Objects.PurgeTenantBatch(ctx,tenant,budget)
  if err!=nil{return false,err}
  if err:=ctx.Err();err!=nil{return false,err}
  more=more||pending
 }
 return more,nil
}
