package interactive

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
)

type previewAdmissionStub struct { ready bool; err error; calls int; sql string }
func (s *previewAdmissionStub) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row { s.calls++; s.sql=sql; return s }
func (s *previewAdmissionStub) Scan(dest ...any) error { if s.err!=nil { return s.err }; *(dest[0].(*bool))=s.ready; return nil }

func TestPreviewRequiresCurrentMediaPoolCPUAdmission(t *testing.T) {
 for _,ready:=range []bool{false,true} {
  stub:=&previewAdmissionStub{ready:ready}
  err:=admitPreviewWorker(context.Background(),stub)
  if ready && err!=nil { t.Fatal(err) }
  if !ready && (err==nil || domain.AsError(err).Code!="OPERATION_UNAVAILABLE") { t.Fatalf("unexpected refusal: %v",err) }
  if strings.Contains(stub.sql,"resource_class='cpu'") { t.Fatal("preview incorrectly admitted against generic CPU pool") }
  for _,required:=range []string{"photo.tenant_capabilities()","operation='editor.preview'","resource_class='media'","supported_backends ? 'cpu'","available_backends ? 'cpu'"} {
   if !strings.Contains(stub.sql,required) { t.Fatalf("missing media/CPU admission predicate %s",required) }
  }
 }
}
func TestPreviewAdmissionPreservesDatabaseFailure(t *testing.T) {
 cause:=errors.New("catalog unavailable");stub:=&previewAdmissionStub{err:cause}
 if !errors.Is(admitPreviewWorker(context.Background(),stub),cause) { t.Fatal("database failure was hidden") }
}
func TestCancelledPreviewDoesNotReadAdmission(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background());cancel();stub:=&previewAdmissionStub{ready:true}
 if !errors.Is(admitPreviewWorker(ctx,stub),context.Canceled) || stub.calls!=0 { t.Fatal("cancelled request read admission") }
}
