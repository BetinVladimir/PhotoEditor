package interchange

import (
 "context"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestArchiveBeginGenerationTicketBindsExactIdentity(t *testing.T) {
 tenantID, uploadID, generationID := uuid.New(),uuid.New(),uuid.New()
 key,_ := objectstore.InterchangeGenerationKey(tenantID,uploadID,generationID)
 ticket := ArchiveBeginGenerationTicket{tenant:tenantID,upload:uploadID,generation:generationID,writer:uuid.New(),reservation:uuid.New(),actor:auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},requestSHA:strings.Repeat("a",64),key:key}
 if !ticket.valid() { t.Fatal("valid ticket refused") }
 changed := ticket; changed.generation = uuid.New()
 if changed.valid() { t.Fatal("generation substitution accepted") }
 changed = ticket; changed.key = ticket.key + "/other"
 if changed.valid() { t.Fatal("key substitution accepted") }
 changed = ticket; changed.writer = uuid.Nil
 if changed.valid() { t.Fatal("missing SQL writer accepted") }
 changed = ticket; changed.requestSHA = strings.Repeat("G",64)
 if changed.valid() { t.Fatal("malformed request hash accepted") }
}

func TestArchiveBeginGenerationSQLMethodsRefuseMalformedTicketBeforeIO(t *testing.T) {
 service := SQLArchiveBeginGenerations{}
 if err := service.Close(context.Background(),ArchiveBeginGenerationTicket{},"multipart"); err != v2input.ErrInvalid { t.Fatal(err) }
 if err := service.Bind(context.Background(),ArchiveBeginGenerationTicket{},"multipart"); err != v2input.ErrInvalid { t.Fatal(err) }
}
