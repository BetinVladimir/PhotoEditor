package workflow

import (
	"encoding/json"
	"photoeditor/backend/internal/encodecontract"
)

// CompileDeliveryEncodeProfile shares the pure compiler with signing validation.
func CompileDeliveryEncodeProfile(node CompiledDownloadNode, sourceRaw []byte, limits DeliveryDerivativeLimits) (json.RawMessage, error) {
	return encodecontract.CompileDeliveryEncodeProfile(node, sourceRaw, encodecontract.DeliveryDerivativeLimits(limits))
}
