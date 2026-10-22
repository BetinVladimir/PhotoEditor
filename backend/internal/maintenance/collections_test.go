package maintenance

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
type collectionClaimRow struct{ids []uuid.UUID;value bool;err error}
func(r collectionClaimRow)Scan(dest ...any)error{if r.err!=nil{return r.err};if len(dest)==1{*dest[0].(*bool)=r.value;return nil};for i,id:=range r.ids{*dest[i].(*uuid.UUID)=id};return nil}
type collectionCleanupDB struct{rows []collectionClaimRow;calls int;args []any;cancel context.CancelFunc}
func(d *collectionCleanupDB)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{row:=d.rows[d.calls];d.calls++;d.args=args;if d.cancel!=nil{d.cancel()};return row}
func TestCollectionCleanerDoesNotFinalizeMissingInvalidOrCancelledClaim(t *testing.T){
 for _,row:=range []collectionClaimRow{{err:pgx.ErrNoRows},{ids:[]uuid.UUID{uuid.Nil,uuid.New(),uuid.New(),uuid.New()}}}{db:=&collectionCleanupDB{rows:[]collectionClaimRow{row}};done,err:=(CollectionCleaner{DB:db}).CleanOne(context.Background());if done||db.calls!=1||row.err==nil&&err==nil{t.Fatal("invalid claim finalized")}}
 ctx,cancel:=context.WithCancel(context.Background());db:=&collectionCleanupDB{rows:[]collectionClaimRow{{ids:[]uuid.UUID{uuid.New(),uuid.New(),uuid.New(),uuid.New()}}},cancel:cancel};_,err:=(CollectionCleaner{DB:db}).CleanOne(ctx);if !errors.Is(err,context.Canceled)||db.calls!=1{t.Fatal("cancelled claim finalized")}
}
func TestCollectionCleanerBindsExactClaimAndHonorsBlockedOrFailedFinalization(t *testing.T){
 ids:=[]uuid.UUID{uuid.New(),uuid.New(),uuid.New(),uuid.New()};for _,last:=range []collectionClaimRow{{value:false},{value:true},{err:errors.New("database")}}{db:=&collectionCleanupDB{rows:[]collectionClaimRow{{ids:ids},last}};done,err:=(CollectionCleaner{DB:db}).CleanOne(context.Background());if db.calls!=2||len(db.args)!=4||db.args[0]!=ids[2]||db.args[1]!=ids[0]||db.args[2]!=ids[3]||db.args[3]!=ids[1]||done!=last.value||(err!=nil)!=(last.err!=nil){t.Fatal("claim identity/finalization mismatch")}}
}
