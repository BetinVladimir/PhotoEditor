package delivery

import (
 "context"
 "testing"
 "github.com/google/uuid"
)
func TestMetadataSourceAPIQueriesCommitAndRefuseSchedulerAuthority(t *testing.T){
 tenant:=uuid.New();tx:=&metadataQueryTx{};pool:=&metadataQueryPool{tx:tx}
 queries:=metadataSourceQueries{db:pool,tenant:tenant};var allowed bool
 query:=`SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type
 FROM photo.v30_delivery_metadata_original($1,$2,$3,$4,$5,$6,$7)`
 if err:=queries.QueryRow(context.Background(),query,tenant,uuid.New(),uuid.New(),uuid.New(),"a",uuid.New(),uuid.New()).Scan(&allowed);err!=nil{t.Fatal(err)}
 if !allowed||pool.begins!=1||len(tx.events)!=4||tx.events[2]!="commit"||tx.events[3]!="rollback"{t.Fatal("API transaction retained locks",tx.events)}
 for _,input:=range []struct{query string;tenant uuid.UUID}{{query,uuid.New()},{`SELECT photo.pin_v30_delivery_metadata_proof($1,$2,$3,$4,$5)`,tenant},{"SELECT true",tenant}}{
  if err:=queries.QueryRow(context.Background(),input.query,input.tenant).Scan(&allowed);err==nil||pool.begins!=1{t.Fatal("foreign tenant/scheduler query accepted")}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=queries.QueryRow(ctx,query,tenant).Scan(&allowed);err!=context.Canceled||pool.begins!=1{t.Fatal("cancel reached DB",err)}
}
