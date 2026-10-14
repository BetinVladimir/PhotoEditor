package workflow

import (
 "context"
 "errors"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationCapacityLeaderExclusiveSession(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated test database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer pool.Close()
 first,err:=AcquireCapacityLeader(ctx,pool);if err!=nil{t.Fatal(err)};defer first.Close()
 second,err:=AcquireCapacityLeader(ctx,pool);if !errors.Is(err,ErrCapacityLeaderUnavailable)||second!=nil{if second!=nil{second.Close()};t.Fatal("second scheduler became leader",err)}
 sentinel:=errors.New("rollback requested")
 if err=first.Transaction(ctx,func(tx pgx.Tx)error{_,e:=tx.Exec(ctx,"CREATE TEMP TABLE capacity_rollback_probe(id integer)");if e!=nil{return e};return sentinel});!errors.Is(err,sentinel){t.Fatal(err)}
 if err=first.Verify(ctx);err!=nil{t.Fatal("logical rollback lost leader",err)}
 // Explicitly lose lock on the owned physical session: Verify must not reacquire.
 if _,err=first.session.Exec(ctx,"SELECT pg_advisory_unlock(2026,32)");err!=nil{t.Fatal(err)}
 called:=false
 if err=first.Transaction(ctx,func(pgx.Tx)error{called=true;return nil});!errors.Is(err,ErrCapacityLeaderUnavailable)||called{t.Fatal("dispatch continued without leader",called,err)}
 replacement,err:=AcquireCapacityLeader(ctx,pool);if err!=nil{t.Fatal(err)};defer replacement.Close()
 if err=replacement.Transaction(ctx,func(tx pgx.Tx)error{var one int;return tx.QueryRow(ctx,"SELECT 1").Scan(&one)});err!=nil{t.Fatal(err)}
 // Losing the lock inside a DB callback must also refuse before COMMIT.
 if err=replacement.Transaction(ctx,func(tx pgx.Tx)error{
  _,e:=tx.Exec(ctx,"SELECT pg_advisory_unlock(2026,32)");return e
 });!errors.Is(err,ErrCapacityLeaderUnavailable){t.Fatal("callback lock loss accepted",err)}
 replacement.Close();replacement.Close()
 if err=replacement.Verify(ctx);!errors.Is(err,ErrCapacityLeaderUnavailable){t.Fatal("closed leader remained usable",err)}
}
