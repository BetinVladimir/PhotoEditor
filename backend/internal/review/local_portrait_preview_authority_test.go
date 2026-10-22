package review

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
)

type localPortraitPreviewAuthorityDB struct {
 row relightReadAuthorityRow
 query string
 args []any
 calls int
}
func (d *localPortraitPreviewAuthorityDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
 d.query=query;d.args=args;d.calls++;return d.row
}

func TestLocalPortraitPreviewRequiresCurrentResultAuthority(t *testing.T) {
 for _,kind:=range []string{"portrait.hair","portrait.clothing"} {
  t.Run(kind,func(t *testing.T){
   who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid,rid:=uuid.New(),uuid.New()
   db:=&localPortraitPreviewAuthorityDB{row:relightReadAuthorityRow{ready:true}}
   if err:=requireRelightReadAuthority(context.Background(),db,who,tid,rid,kind);err!=nil{t.Fatal(err)}
   want:=[]any{tid,rid,who.UserID,who.SessionID,kind}
   if db.calls!=1||len(db.args)!=len(want){t.Fatal("missing principal-bound authority")}
   for i:=range want{if db.args[i]!=want[i]{t.Fatalf("incorrect binding %d",i)}}
   for _,predicate:=range []string{"v.tenant_id=$1","v.id=$2","v.kind=$5","v.state IN('pending','decided')","photo.v28_local_portrait_result_ready($1,v.run_id,$3,$4)"}{
    if !strings.Contains(db.query,predicate){t.Fatalf("missing exact authority predicate %s",predicate)}
   }
   if !candidatePreviewKind(kind){t.Fatal("local portrait media route disabled")}
   for _,variant:=range []string{"preview1600","preview2560"}{
    for _,side:=range []string{"before","after"}{
     if candidatePreviewNodeKey(kind,side,variant)!="render.derivative:"+variant{t.Fatal("incorrect adopted derivative node")}
     if candidatePreviewAssetFallbackAllowed(kind,side)!=(side=="before"){t.Fatal("candidate must never fall back to accepted base")}
    }
   }
   origin,receipt:=candidatePreviewSQLColumns(kind)
   if origin!="r.v26_derivative_task_id"||receipt!="d.v26_derivative_receipt"{t.Fatal("incorrect canonical derivative receipt")}
  })
 }
}

func TestLocalPortraitPreviewRefusesRevokedAuthorityAndPreservesTransientFailure(t *testing.T){
 unavailable:=errors.New("database transport unavailable")
 for _,kind:=range []string{"portrait.hair","portrait.clothing"}{
  for _,row:=range []relightReadAuthorityRow{{ready:false},{err:unavailable}}{
   db:=&localPortraitPreviewAuthorityDB{row:row}
   err:=requireRelightReadAuthority(context.Background(),db,auth.Identity{},uuid.New(),uuid.New(),kind)
   if err==nil{t.Fatal("revoked local authority accepted")}
   if row.err!=nil&&!errors.Is(err,unavailable){t.Fatal("transient authority failure hidden")}
  }
  ctx,cancel:=context.WithCancel(context.Background());cancel()
  db:=&localPortraitPreviewAuthorityDB{}
  if err:=requireRelightReadAuthority(ctx,db,auth.Identity{},uuid.New(),uuid.New(),kind);!errors.Is(err,context.Canceled)||db.calls!=0{t.Fatal("cancelled preview queried authority")}
 }
}
