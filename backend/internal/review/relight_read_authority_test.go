package review

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
)

type relightReadAuthorityRow struct { ready bool; err error }
func (r relightReadAuthorityRow) Scan(dest ...any) error {
 if r.err != nil { return r.err }
 *dest[0].(*bool) = r.ready
 return nil
}
type relightReadAuthorityDB struct { row relightReadAuthorityRow; calls int; args []any }
func (d *relightReadAuthorityDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
 d.calls++; d.args=args; return d.row
}

func TestRelightReadAuthorityBindsCurrentPrincipalAndReview(t *testing.T) {
 who := auth.Identity{UserID:uuid.New(), SessionID:uuid.New()}
 tid, rid := uuid.New(), uuid.New()
 db := &relightReadAuthorityDB{row:relightReadAuthorityRow{ready:true}}
 if err := requireRelightReadAuthority(context.Background(),db,who,tid,rid,"image.relight"); err != nil { t.Fatal(err) }
 want := []any{tid,rid,who.UserID,who.SessionID}
 if db.calls != 1 || len(db.args) != len(want) { t.Fatal("missing authority query") }
 for i := range want { if db.args[i] != want[i] { t.Fatalf("wrong authority binding %d",i) } }
}
func TestRelightReadAuthorityRejectsRevokedResultAndDatabaseFailure(t *testing.T) {
 unavailable := errors.New("authority unavailable")
 for _, row := range []relightReadAuthorityRow{{ready:false},{err:unavailable}} {
  db := &relightReadAuthorityDB{row:row}
  err := requireRelightReadAuthority(context.Background(),db,auth.Identity{},uuid.New(),uuid.New(),"image.relight")
  if err == nil { t.Fatal("authority failure accepted") }
  if row.err != nil && !errors.Is(err,unavailable) { t.Fatal("database failure hidden") }
 }
}
func TestRelightReadAuthorityLeavesOtherReviewKindsUnchanged(t *testing.T) {
 db := &relightReadAuthorityDB{}
 if err := requireRelightReadAuthority(context.Background(),db,auth.Identity{},uuid.New(),uuid.New(),"portrait.open_eyes"); err != nil || db.calls != 0 { t.Fatal("unrelated review queried relight authority") }
}
func TestRelightReadAuthorityCancellationAvoidsQuery(t *testing.T) {
 ctx,cancel := context.WithCancel(context.Background()); cancel()
 db := &relightReadAuthorityDB{}
 if err := requireRelightReadAuthority(ctx,db,auth.Identity{},uuid.New(),uuid.New(),"image.relight"); !errors.Is(err,context.Canceled) || db.calls != 0 { t.Fatal("cancelled read reached database") }
}
