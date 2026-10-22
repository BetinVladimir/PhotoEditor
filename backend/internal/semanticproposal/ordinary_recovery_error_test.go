package semanticproposal

import (
 "context"
 "errors"
 "strings"
 "testing"
 "photoeditor/backend/internal/v2input"
)

func TestOrdinaryRecoveryDiagnosticsPreserveCausesAndClosedLabels(t *testing.T) {
 causes := []error{v2input.ErrConflict, v2input.ErrInvalid, context.Canceled, context.DeadlineExceeded, errors.New("fixture underlying error")}
 stages := []string{"validate", "receipt_locator", "accepted_receipt", "accepted_receipt_missing", "load_anchor", "resolve_catalog", "restore_person", "continuation", "policy_scope", "frozen_configuration", "capacity", "select_protection", "scene_parent", "admit_parent"}
 for _, stage := range stages {
  for _, cause := range causes {
   got := ordinaryRecoveryError(stage, cause)
   if !errors.Is(got, cause) || !strings.HasPrefix(got.Error(), "ordinary parent recovery "+stage+": ") { t.Fatalf("stage/cause lost: %q %v",stage,got) }
  }
  if ordinaryRecoveryError(stage,nil) != nil {t.Fatal("nil success changed")}
 }
 if got:=ordinaryRecoveryError("untrusted-stage-must-not-appear",v2input.ErrConflict); !errors.Is(got,v2input.ErrConflict) || strings.Contains(got.Error(),"untrusted-stage") {t.Fatal("unclosed stage logged",got)}
}
