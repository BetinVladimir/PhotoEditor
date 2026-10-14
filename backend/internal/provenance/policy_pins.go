package provenance

import (
	"github.com/google/uuid"
	"sort"
)

// InstalledPolicyPins is a deployment snapshot for scheduler readiness. It
// does not replace invocation-time operator, revocation or expiry checks.
type InstalledPolicyPin struct {
	PolicyID  uuid.UUID `json:"policy_id"`
	PolicySHA string    `json:"policy_sha"`
}

func (r *SigningPolicyRegistry) InstalledPolicyPins() ([]InstalledPolicyPin, error) {
	if r == nil || r.authority == nil || len(r.policies) == 0 {
		return nil, ErrInvalid
	}
	pins := make([]InstalledPolicyPin, 0, len(r.policies))
	for id, installed := range r.policies {
		if id == uuid.Nil || !digest(installed.ref.SHA256) {
			return nil, ErrInvalid
		}
		pins = append(pins, InstalledPolicyPin{PolicyID: id, PolicySHA: installed.ref.SHA256})
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].PolicyID.String() < pins[j].PolicyID.String() })
	return pins, nil
}
