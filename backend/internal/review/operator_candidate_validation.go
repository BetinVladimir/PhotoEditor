package review

// ValidateOperatorCandidatePair reuses the exact physical candidate/base
// validation used by Service.Decide. It does not persist or authorize a decision.
func ValidateOperatorCandidatePair(kind string, candidate, base []byte) error {
	return validateCandidatePair(kind, candidate, base)
}
