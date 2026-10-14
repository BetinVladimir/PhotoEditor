package semanticproposal

import "photoeditor/backend/internal/v2input"

// Reserve the complete document byte budget before IO; refusal exposes no
// partial reservation. Source, selection and protection are all obligatory.
func nativeTransferDocumentBudget(objects []personEvidenceObject, limit int64) (int64, error) {
	if len(objects) != 3 || limit <= 0 || limit > 9007199254740991 {
		return 0, v2input.ErrInvalid
	}
	var used int64
	for _, pin := range objects {
		if pin.object.Bytes < 2 || pin.object.Bytes > 8<<20 {
			return 0, v2input.ErrInvalid
		}
		if err := chargeMetadata(&used, limit, int(pin.object.Bytes)); err != nil {
			return 0, err
		}
	}
	return used, nil
}
