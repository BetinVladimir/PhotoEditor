package database

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

type roleResult struct{safe bool;err error}
func (r roleResult) Scan(dest ...any)error{if r.err!=nil{return r.err};*(dest[0].(*bool))=r.safe;return nil}
type roleQuery func(context.Context,string,...any)pgx.Row
func (fn roleQuery) QueryRow(ctx context.Context,query string,args ...any)pgx.Row{return fn(ctx,query,args...)}

func TestRoleIsolationRequiresPositiveBoundedCurrentEvidence(t *testing.T){
 for _,role:=range []string{"photo_api","photo_auth","photo_portal","photo_worker","photo_scheduler","photo_publisher","photo_maintenance","photo_gateway","photo_mail","photo_semantic_admission"}{
  for _,safe:=range []bool{false,true}{
   err:=RequireApplicationRole(context.Background(),roleQuery(func(ctx context.Context,query string,args ...any)pgx.Row{
    deadline,ok:=ctx.Deadline();if !ok||time.Until(deadline)>5*time.Second||len(args)!=1||args[0]!=role{t.Fatal("unbounded or unbound role guard")};return roleResult{safe:safe}
   }),role)
   if (err==nil)!=safe{t.Fatal(role,safe,err)}
  }
 }
}
func TestInvalidRoleAndCancelledContextNeverQuery(t *testing.T){
 noIO:=roleQuery(func(context.Context,string,...any)pgx.Row{t.Fatal("unexpected role query");return roleResult{}})
 for _,role:=range []string{"","photo_fn_owner","postgres","photo_api;RESET ROLE"}{if RequireApplicationRole(context.Background(),noIO,role)==nil{t.Fatal("invalid role",role)}}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if !errors.Is(RequireApplicationRole(ctx,noIO,"photo_api"),context.Canceled){t.Fatal("cancellation lost")}
}

func TestConnectionGuardPreservesInitializerFailure(t *testing.T) {
 config,err:=pgxpool.ParseConfig("postgres://localhost/photo");if err!=nil{t.Fatal(err)}
 config.ConnConfig.RuntimeParams["role"]="photo_api"
 sentinel:=errors.New("initializer failed"); calls:=0
 config.AfterConnect=func(context.Context,*pgx.Conn)error{calls++;return sentinel}
 GuardApplicationRole(config)
 // A failed prior initializer must abort before the role query (nil connection).
 if err=config.AfterConnect(context.Background(),nil);!errors.Is(err,sentinel)||calls!=1{t.Fatal("initializer chain lost",err,calls)}
}
