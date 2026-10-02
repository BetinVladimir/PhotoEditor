package v2input

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func CaptureRequiredPeople(shots CaptureShotList) ([]uuid.UUID, error) {
	if _, err := validateCaptureShots(shots); err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]bool{}
	for _, requirement := range shots.Requirements {
		for _, person := range requirement.PersonIDs {
			seen[person] = true
		}
	}
	if len(seen) > 5000 {
		return nil, ErrInvalid
	}
	people := make([]uuid.UUID, 0, len(seen))
	for person := range seen {
		people = append(people, person)
	}
	sort.Slice(people, func(i, j int) bool { return people[i].String() < people[j].String() })
	return people, nil
}

// CompileCaptureConsentSnapshot allows matching grants only, for the exact
// receipt version and required shot identities. It never confirms a new match.
func CompileCaptureConsentSnapshot(raw []byte, version uuid.UUID, shots CaptureShotList) ([]byte, []uuid.UUID, error) {
	required, err := CaptureRequiredPeople(shots)
	if err != nil || version == uuid.Nil || !exactJSONKeys(raw, "schema_version", "grants", "confirmed_faces") || validateSessionConsentSnapshot(raw) != nil {
		return nil, nil, ErrInvalid
	}
	var doc struct {
		SchemaVersion  int                    `json:"schema_version"`
		Grants         []sessionConsentGrant  `json:"grants"`
		ConfirmedFaces []sessionConfirmedFace `json:"confirmed_faces"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil, nil, ErrInvalid
	}
	granted := map[uuid.UUID]bool{}
	for _, grant := range doc.Grants {
		if grant.Purpose != "identity_matching" || grant.PersonID == nil || granted[*grant.PersonID] {
			return nil, nil, ErrInvalid
		}
		granted[*grant.PersonID] = true
	}
	used := map[uuid.UUID]bool{}
	for _, person := range required {
		if !granted[person] {
			return nil, nil, ErrInvalid
		}
		used[person] = true
	}
	confirmed := map[uuid.UUID]bool{}
	for _, face := range doc.ConfirmedFaces {
		if face.AssetVersionID != version {
			return nil, nil, ErrInvalid
		}
		confirmed[face.PersonID] = true
		used[face.PersonID] = true
	}
	if len(confirmed) > 100 || len(used) != len(granted) {
		return nil, nil, ErrInvalid
	}
	people := make([]uuid.UUID, 0, len(confirmed))
	for person := range confirmed {
		people = append(people, person)
	}
	sort.Slice(people, func(i, j int) bool { return people[i].String() < people[j].String() })
	body, err := canonical.Bytes(raw)
	return body, people, err
}

func (s SQLCaptureAssessmentStore) FreezeConsents(ctx context.Context, lease CaptureAssessmentLease, shots CaptureShotList) ([]byte, []uuid.UUID, error) {
	if s.DB == nil || !validCaptureLease(lease) {
		return nil, nil, ErrInvalid
	}
	people, err := CaptureRequiredPeople(shots)
	if err != nil {
		return nil, nil, err
	}
	var raw []byte
	if err = s.DB.QueryRow(ctx, `SELECT photo.freeze_capture_consents($1,$2,$3,$4::uuid[])`, lease.TenantID, lease.JobID, lease.Token, people).Scan(&raw); err != nil {
		return nil, nil, err
	}
	if len(raw) == 0 {
		return nil, nil, ErrExpired
	}
	return CompileCaptureConsentSnapshot(raw, lease.AssetVersionID, shots)
}
