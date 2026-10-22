package collection

import (
 "context"
 "testing"
 "time"
)

// Authored only. A page must not attach the pre-wait revision to rows ordered
// using a later committed sort mode under READ COMMITTED.
func TestIntegrationGalleryReadsRevisionAfterAwaitedCollectionMutation(t *testing.T) {
 ctx,admin,service,who,tenant,collection,_:=sectionAbandonFixture(t)
 work,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 blocker,err:=admin.Begin(work);if err!=nil{t.Fatal(err)};defer blocker.Rollback(context.Background())
 var pid int
 if err=blocker.QueryRow(work,`SELECT pg_backend_pid()`).Scan(&pid);err!=nil{t.Fatal(err)}
 if _,err=blocker.Exec(work,`UPDATE photo.collections SET sort_mode='filename',revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tenant,collection);err!=nil{t.Fatal(err)}
 type outcome struct{ revision string; err error }
 done:=make(chan outcome,1)
 go func(){page,e:=service.ListItems(work,who,tenant,collection,10,"","","","","");done<-outcome{page.Revision,e}}()
 ticker:=time.NewTicker(10*time.Millisecond);defer ticker.Stop()
 for {
  var waiting bool
  if err=admin.QueryRow(work,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,pid).Scan(&waiting);err!=nil{t.Fatal(err)}
  if waiting{break}
  select{case got:=<-done:t.Fatalf("page escaped collection revision lock: %+v",got);case <-ticker.C:case <-work.Done():t.Fatal(work.Err())}
 }
 if err=blocker.Commit(work);err!=nil{t.Fatal(err)}
 select{case got:=<-done:if got.err!=nil||got.revision!="2"{t.Fatalf("page returned stale revision: %+v",got)};case <-work.Done():t.Fatal(work.Err())}
}
