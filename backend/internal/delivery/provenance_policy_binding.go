package delivery

import (
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

type FrozenSigningChoice struct {
 PolicyID uuid.UUID
 PolicySHA string
}

// DeliverySigningChoice reads only the immutable Delivery policy. Operator
// qualification is a separate live V31 check at launch and adoption.
func DeliverySigningChoice(frozen FrozenInput)(FrozenSigningChoice,error){
 if frozen.SchemaVersion!=1||frozen.Policy.Provenance!="required"||
  !validDeliverySigningPolicyChoice(frozen.Policy){return FrozenSigningChoice{},v2input.ErrInvalid}
 return FrozenSigningChoice{PolicyID:frozen.Policy.SigningPolicyID,
  PolicySHA:frozen.Policy.SigningPolicySHA},nil
}
