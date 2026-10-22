package review
import("context";"errors";"testing";"github.com/google/uuid";"photoeditor/backend/internal/auth")
func TestReflectionReadAuthorityBindsCurrentPrincipalAndReview(t *testing.T) {
 who := auth.Identity{UserID:uuid.New(), SessionID:uuid.New()}
 tid, rid := uuid.New(), uuid.New()
 db := &relightReadAuthorityDB{row:relightReadAuthorityRow{ready:true}}
 if err := requireRelightReadAuthority(context.Background(),db,who,tid,rid,"image.reflection"); err != nil { t.Fatal(err) }
 want := []any{tid,rid,who.UserID,who.SessionID}
 if db.calls != 1 || len(db.args) != len(want) { t.Fatal("missing authority query") }
 for i := range want { if db.args[i] != want[i] { t.Fatalf("wrong authority binding %d",i) } }
}
func TestReflectionReadAuthorityRejectsRevokedResultAndDatabaseFailure(t *testing.T) {
 unavailable := errors.New("authority unavailable")
 for _, row := range []relightReadAuthorityRow{{ready:false},{err:unavailable}} {
  db := &relightReadAuthorityDB{row:row}
  err := requireRelightReadAuthority(context.Background(),db,auth.Identity{},uuid.New(),uuid.New(),"image.reflection")
  if err == nil { t.Fatal("authority failure accepted") }
  if row.err != nil && !errors.Is(err,unavailable) { t.Fatal("database failure hidden") }
 }
}
func TestReflectionReadAuthorityCancellationAvoidsQuery(t *testing.T) {
 ctx,cancel := context.WithCancel(context.Background()); cancel()
 db := &relightReadAuthorityDB{}
 if err := requireRelightReadAuthority(ctx,db,auth.Identity{},uuid.New(),uuid.New(),"image.reflection"); !errors.Is(err,context.Canceled) || db.calls != 0 { t.Fatal("cancelled read reached database") }
}
