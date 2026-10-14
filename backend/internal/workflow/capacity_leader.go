package workflow

import (
 "context"
 "errors"
 "time"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

var ErrCapacityLeaderUnavailable=errors.New("capacity scheduler leadership unavailable")

// CapacityLeader owns one physical session. Only its serialized leader loop may
// use it; transaction callbacks must perform DB-only work and never change locks.
type CapacityLeader struct { session *pgxpool.Conn }

func AcquireCapacityLeader(ctx context.Context,pool *pgxpool.Pool)(*CapacityLeader,error){
 if pool==nil{return nil,ErrCapacityLeaderUnavailable}
 session,err:=pool.Acquire(ctx);if err!=nil{return nil,err}
 leader:=&CapacityLeader{session:session}
 var held bool
 err=session.QueryRow(ctx,"SELECT pg_try_advisory_lock(2026,32)").Scan(&held)
 if err!=nil{leader.Close();return nil,err}
 if !held{session.Release();leader.session=nil;return nil,ErrCapacityLeaderUnavailable}
 return leader,nil
}

const capacityLeaderProof=`SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_locks
 WHERE locktype='advisory' AND pid=pg_backend_pid() AND granted
 AND classid=2026::oid AND objid=32::oid AND objsubid=2
 AND mode='ExclusiveLock' AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database()))`

// Close destroys the session rather than returning an advisory-locked connection
// to the pool. This also handles unknown outcomes of lock acquisition or COMMIT.
func(l *CapacityLeader) Close(){
 if l==nil||l.session==nil{return}
 session:=l.session;l.session=nil
 conn:=session.Hijack()
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
 _=conn.Close(ctx)
}

func(l *CapacityLeader) Verify(ctx context.Context)error{
 if l==nil||l.session==nil{return ErrCapacityLeaderUnavailable}
 var held bool
 if err:=l.session.QueryRow(ctx,capacityLeaderProof).Scan(&held);err!=nil{l.Close();return err}
 if !held{l.Close();return ErrCapacityLeaderUnavailable}
 return nil
}

// Transaction never retries a commit with an uncertain outcome. Its caller must
// discard in-memory deficits and reconcile the durable dispatch/outbox state.
func(l *CapacityLeader) Transaction(ctx context.Context,fn func(pgx.Tx)error)error{
 if fn==nil{return errors.New("capacity dispatch callback required")}
 if err:=l.Verify(ctx);err!=nil{return err}
 tx,err:=l.session.BeginTx(ctx,pgx.TxOptions{});if err!=nil{l.Close();return err}
 rollback:=func(){cleanup,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel();if err:=tx.Rollback(cleanup);err!=nil&&!errors.Is(err,pgx.ErrTxClosed){l.Close()}}
 defer rollback()
 var held bool
 if err=tx.QueryRow(ctx,capacityLeaderProof).Scan(&held);err!=nil||!held{l.Close();if err!=nil{return err};return ErrCapacityLeaderUnavailable}
 if err=fn(tx);err!=nil{return err}
 if err=tx.QueryRow(ctx,capacityLeaderProof).Scan(&held);err!=nil||!held{l.Close();if err!=nil{return err};return ErrCapacityLeaderUnavailable}
 if err=tx.Commit(ctx);err!=nil{l.Close();return err}
 return nil
}
