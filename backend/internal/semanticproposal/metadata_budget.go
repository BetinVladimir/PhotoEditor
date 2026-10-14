package semanticproposal

import "photoeditor/backend/internal/v2input"

// MetadataBytes limits retained raw document/profile bytes, with headroom for
// compiler parsing and ownership copies. It is not a measured total heap bound.
func validMetadataBudget(configuration TransferPreparation) bool {
	return configuration.RAM > 0 && configuration.MetadataBytes > 0 && configuration.MetadataBytes <= configuration.RAM/8
}

func chargeMetadata(used *int64, limit int64, lengths ...int) error {
	if used == nil || limit <= 0 || *used < 0 || *used > limit {
		return v2input.ErrInvalid
	}
	next := *used
	for _, length := range lengths {
		if length < 0 || int64(length) > limit-next {
			return v2input.ErrInvalid
		}
		next += int64(length)
	}
	*used = next
	return nil
}
