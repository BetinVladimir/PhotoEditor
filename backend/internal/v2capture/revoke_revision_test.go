package v2capture

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// Invalid wire revisions must be refused before any database/receipt access.
func TestRevokeDeviceRejectsNoncanonicalRevisionBeforeDatabase(t *testing.T) {
 service:=Service{DB:&pgxpool.Pool{}}
 for _,revision:=range []string{"+1","01","-0","0","-1"," 1","1 ","9223372036854775808"} {
  t.Run(revision,func(t *testing.T){
   _,err:=service.Revoke(context.Background(),auth.Identity{},uuid.New(),uuid.New(),"stable-revoke-key",RevokeRequest{ExpectedRevision:revision})
   if err==nil||domain.AsError(err).Code!="VALIDATION_FAILED" { t.Fatal("noncanonical revision reached database",revision,err) }
  })
 }
}
