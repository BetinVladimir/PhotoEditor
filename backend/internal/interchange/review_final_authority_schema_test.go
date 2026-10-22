package interchange

import (
 "context"
 "errors"
 "strings"
 "testing"
 "time"

 "github.com/jackc/pgx/v5"
)

type finalReviewRow struct{ ready bool; err error; after func() }
func(r finalReviewRow)Scan(dst ...any)error{if r.after!=nil{r.after()};*(dst[0].(*bool))=r.ready;return r.err}
type finalReviewReader struct{ready bool; err error; after func(); calls int; query string; bounded bool}
func(r *finalReviewReader)QueryRow(ctx context.Context,q string,_ ...any)pgx.Row{
 r.calls++;r.query=q;deadline,ok:=ctx.Deadline();r.bounded=ok&&time.Until(deadline)<=5*time.Second
 return finalReviewRow{r.ready,r.err,r.after}
}
func TestImportReviewInstallationRequiresExactEntryPointsAndFinalFences(t *testing.T){
 for _,ready:=range []bool{false,true}{
  r:=&finalReviewReader{ready:ready}
  err:=RequireImportReviewFinalAuthority(context.Background(),r)
  if (err==nil)!=ready||!r.bounded||r.calls!=1{t.Fatalf("ready=%v bounded=%v calls=%d err=%v",ready,r.bounded,r.calls,err)}
  for _,fragment:=range []string{"count(*)=2","p.prosecdef","photo_fn_owner","a.grantee NOT IN","FOR UPDATE;","a.expires_at>clock_timestamp()"," IF NOT FOUND OR old.proposal_sha<>proposal_sha"}{
   if !strings.Contains(r.query,fragment){t.Fatalf("missing installed contract %s",fragment)}
  }
 }
}
func TestImportReviewInstallationCancellationAndDatabaseFailure(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 r:=&finalReviewReader{ready:true}
 if err:=RequireImportReviewFinalAuthority(ctx,r);!errors.Is(err,context.Canceled)||r.calls!=0{t.Fatalf("err=%v calls=%d",err,r.calls)}
 ctx,cancel=context.WithCancel(context.Background());defer cancel()
 r=&finalReviewReader{ready:true,after:cancel}
 if err:=RequireImportReviewFinalAuthority(ctx,r);!errors.Is(err,context.Canceled){t.Fatal(err)}
 failure:=errors.New("catalog unavailable");r=&finalReviewReader{err:failure}
 if err:=RequireImportReviewFinalAuthority(context.Background(),r);!errors.Is(err,failure){t.Fatal(err)}
 if err:=RequireImportReviewFinalAuthority(context.Background(),nil);err==nil{t.Fatal("nil database accepted")}
}
