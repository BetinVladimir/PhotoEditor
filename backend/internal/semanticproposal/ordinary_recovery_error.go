package semanticproposal

import "fmt"

// Closed stage labels localize recovery failure without request/body/identity
// logging. Wrapped causes remain available through errors.Is/errors.As.
func ordinaryRecoveryError(stage string, err error) error {
 if err == nil { return nil }
 switch stage {
 case "validate", "receipt_locator", "accepted_receipt", "accepted_receipt_missing", "load_anchor", "resolve_catalog", "restore_person", "continuation", "policy_scope", "frozen_configuration", "capacity", "select_protection", "scene_parent", "admit_parent":
  return fmt.Errorf("ordinary parent recovery %s: %w", stage, err)
 default:
  return fmt.Errorf("ordinary parent recovery: %w", err)
 }
}
