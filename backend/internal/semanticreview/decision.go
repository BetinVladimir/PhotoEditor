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

type PreviewSelection struct {
	Render   TargetDocument `json:"render"`
	Evidence TargetDocument `json:"evidence"`
}

type TargetDecision struct {
	TargetID uuid.UUID         `json:"target_id"`
	Decision string            `json:"decision"`
	Preview  *PreviewSelection `json:"preview,omitempty"`
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
	if !validRevision(result.ExpectedRevision) || result.ExpectedRevision == "9007199254740991" || !shaPattern.MatchString(result.PlanSHA) || len(result.TargetDecisions) < 1 || len(result.TargetDecisions) > 100 {
		return ApplyDecision{}, ErrInvalid
	}
	// An omitted preview remains valid for a rejection (and for legacy
	// non-shown approval routes). Explicit null is not an omitted field: the
	// public client contract requires an object whenever preview is supplied.
	var supplied struct {
		TargetDecisions []struct {
			Preview json.RawMessage `json:"preview"`
		} `json:"target_decisions"`
	}
	if json.Unmarshal(raw, &supplied) != nil {
		return ApplyDecision{}, ErrInvalid
	}
	for _, item := range supplied.TargetDecisions {
		if bytes.Equal(bytes.TrimSpace(item.Preview), []byte("null")) {
			return ApplyDecision{}, ErrInvalid
		}
	}
	seen := map[uuid.UUID]bool{}
	for _, item := range result.TargetDecisions {
		if item.TargetID == uuid.Nil || seen[item.TargetID] || (item.Decision != "approve" && item.Decision != "reject") {
			return ApplyDecision{}, ErrInvalid
		}
		if item.Preview != nil {
			p := item.Preview
			if item.Decision != "approve" || p.Render.ArtifactID == uuid.Nil || p.Evidence.ArtifactID == uuid.Nil ||
				p.Render.ArtifactID == p.Evidence.ArtifactID || !shaPattern.MatchString(p.Render.SHA256) || !shaPattern.MatchString(p.Evidence.SHA256) {
				return ApplyDecision{}, ErrInvalid
			}
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
	return decodeStrictWithLimit(raw, target, 65536)
}

// Artifact readers may use their explicit contract limit without widening the
// request DTO limit. Strict schema and canonical JSON checks remain identical.
func decodeStrictWithLimit(raw []byte, target any, maximum int) error {
	if maximum < 1 || maximum > 1048576 || len(raw) == 0 || len(raw) > maximum {
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

// SQL receipts are typed JSONB, not JCS artifacts. Decode integer columns
// directly so PostgreSQL int64 fences never pass through float64 or JCS.
func decodeSQLReceipt(raw []byte, target any) error {
	if len(raw) < 2 || len(raw) > 16384 || !json.Valid(raw) {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
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
