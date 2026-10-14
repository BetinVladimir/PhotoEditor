package semanticreview

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/canonical"
	"regexp"
	"sort"
	"strconv"
)

var ErrInvalid = errors.New("invalid semantic review decision")
var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type TargetDecision struct {
	TargetID uuid.UUID `json:"target_id"`
	Decision string    `json:"decision"`
}
type ApplyDecision struct {
	PlanSHA          string           `json:"plan_sha"`
	TargetDecisions  []TargetDecision `json:"target_decisions"`
	ExpectedRevision string           `json:"expected_revision"`
}

func DecodeApplyDecision(raw []byte) (ApplyDecision, error) {
	var result ApplyDecision
	if decodeStrict(raw, &result) != nil {
		return result, ErrInvalid
	}
	if !validRevision(result.ExpectedRevision) || !shaPattern.MatchString(result.PlanSHA) || len(result.TargetDecisions) < 1 || len(result.TargetDecisions) > 100 {
		return ApplyDecision{}, ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, item := range result.TargetDecisions {
		if item.TargetID == uuid.Nil || seen[item.TargetID] || (item.Decision != "approve" && item.Decision != "reject") {
			return ApplyDecision{}, ErrInvalid
		}
		seen[item.TargetID] = true
	}
	return result, nil
}
func validRevision(value string) bool {
	if value == "" || len(value) > 19 || value[0] == '0' {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed > 0 && parsed <= 9007199254740991
}

// ValidateSelection requires a server-owned plan SHA, revision and candidate states.
// It returns only explicit decisions; omitted targets receive no mutation.
func ValidateSelection(decision ApplyDecision, planSHA, revision string, states map[uuid.UUID]string) ([]TargetDecision, error) {
	raw, err := canonical.Marshal(decision)
	if err != nil {
		return nil, ErrInvalid
	}
	owned, err := DecodeApplyDecision(raw)
	if err != nil || owned.PlanSHA != planSHA || owned.ExpectedRevision != revision || !validRevision(revision) {
		return nil, ErrInvalid
	}
	if len(states) < 1 || len(states) > 5000 {
		return nil, ErrInvalid
	}
	for id, state := range states {
		if id == uuid.Nil || !validTargetState(state) {
			return nil, ErrInvalid
		}
	}
	for _, item := range owned.TargetDecisions {
		state, exists := states[item.TargetID]
		if !exists || state != "proposed" {
			return nil, ErrInvalid
		}
	}
	result := append([]TargetDecision(nil), owned.TargetDecisions...)
	sort.Slice(result, func(i, j int) bool { return result[i].TargetID.String() < result[j].TargetID.String() })
	return result, nil
}

func decodeStrict(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > 65536 {
		return ErrInvalid
	}
	encoded, err := canonical.Bytes(raw)
	if err != nil {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ErrInvalid
	}
	return nil
}

func validTargetState(state string) bool {
	switch state {
	case "proposed", "unsupported", "rejected", "queued", "applied", "failed", "cancelled":
		return true
	default:
		return false
	}
}
