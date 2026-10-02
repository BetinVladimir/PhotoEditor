package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

type dustCrop struct {
	X      int64 `json:"x"`
	Y      int64 `json:"y"`
	Width  int64 `json:"width"`
	Height int64 `json:"height"`
}
type dustCandidate struct {
	ID         string  `json:"id"`
	SensorX    float64 `json:"sensor_x"`
	SensorY    float64 `json:"sensor_y"`
	Radius     float64 `json:"radius_sensor_px"`
	Observed   int     `json:"observed_frames"`
	Eligible   int     `json:"eligible_frames"`
	Recurrence float64 `json:"recurrence"`
	Decision   string  `json:"decision"`
}
type dustDocument struct {
	Frames       []ArtifactRef   `json:"frames"`
	CameraID     string          `json:"camera_id"`
	SensorWidth  int64           `json:"sensor_width"`
	SensorHeight int64           `json:"sensor_height"`
	RawCrop      dustCrop        `json:"raw_crop"`
	Algorithm    string          `json:"algorithm"`
	Candidates   []dustCandidate `json:"candidates"`
	Warnings     []string        `json:"warnings"`
}
type VerifiedDustProposal struct {
	ref      ArtifactRef
	document dustDocument
}

func ReadDustProposal(ctx context.Context, reader io.Reader, ref, base ArtifactRef) (VerifiedDustProposal, error) {
	fail := func() (VerifiedDustProposal, error) {
		return VerifiedDustProposal{}, errors.New("invalid sensor dust proposal")
	}
	if !ref.valid() || !base.valid() || reader == nil {
		return fail()
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 1048577))
	if err != nil {
		return VerifiedDustProposal{}, err
	}
	if len(raw) > 1048576 {
		return fail()
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return fail()
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return fail()
	}
	var d dustDocument
	dec := json.NewDecoder(bytes.NewReader(normalized))
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil {
		return fail()
	}
	round, _ := canonical.Marshal(d)
	if !bytes.Equal(round, normalized) || d.Algorithm != "sensor_dust_log_v1" || !strings.HasPrefix(d.CameraID, "raw-") || len(d.CameraID) != 68 || len(d.Frames) < 5 || len(d.Frames) > 64 || len(d.Candidates) > 4096 || len(d.Warnings) > 1 || d.SensorWidth < 8 || d.SensorHeight < 8 || d.RawCrop.Width < 8 || d.RawCrop.Height < 8 || d.RawCrop.X < 0 || d.RawCrop.Y < 0 || d.RawCrop.X+d.RawCrop.Width > d.SensorWidth || d.RawCrop.Y+d.RawCrop.Height > d.SensorHeight {
		return fail()
	}
	if _, err = domain.ParseDigest(strings.TrimPrefix(d.CameraID, "raw-")); err != nil {
		return fail()
	}
	seenFrames := map[uuid.UUID]bool{}
	hasBase := false
	hashes := make([]string, len(d.Frames))
	for i, f := range d.Frames {
		if !f.valid() || seenFrames[f.ID] {
			return fail()
		}
		seenFrames[f.ID] = true
		hasBase = hasBase || f == base
		hashes[i] = f.SHA256
	}
	if !hasBase {
		return fail()
	}
	seen := map[string]bool{}
	for _, c := range d.Candidates {
		if err = ctx.Err(); err != nil {
			return VerifiedDustProposal{}, err
		}
		if _, err = domain.ParseDigest(c.ID); err != nil || seen[c.ID] || c.Decision != "pending" || math.IsNaN(c.SensorX) || math.IsNaN(c.SensorY) || math.IsNaN(c.Radius) || c.SensorX < float64(d.RawCrop.X) || c.SensorX >= float64(d.RawCrop.X+d.RawCrop.Width) || c.SensorY < float64(d.RawCrop.Y) || c.SensorY >= float64(d.RawCrop.Y+d.RawCrop.Height) || c.Radius <= 0 || c.Radius > 32 || c.Eligible != len(d.Frames) || c.Observed < 1 || c.Observed > c.Eligible || c.Recurrence < .8 || c.Recurrence > 1 || math.Abs(c.Recurrence-float64(c.Observed)/float64(c.Eligible)) > 1e-12 {
			return fail()
		}
		expected, e := canonical.Hash(map[string]any{"algorithm": "sensor_dust_log_v1", "camera_id": d.CameraID, "sensor": []int64{d.SensorWidth, d.SensorHeight}, "x": c.SensorX, "y": c.SensorY, "radius": c.Radius, "frames": hashes})
		if e != nil || expected != c.ID {
			return fail()
		}
		seen[c.ID] = true
	}
	if (len(d.Candidates) == 0) != (len(d.Warnings) == 1 && d.Warnings[0] == "no_repeated_dust") {
		return fail()
	}
	return VerifiedDustProposal{ref: ref, document: d}, nil
}

type DustSnapshot struct {
	TenantID, ReviewID, RunID uuid.UUID
	Revision                  int64
	State                     string
	Proposal, Base            ArtifactRef
	CandidateIDs              []string
}
type DustSelection struct {
	Version  string      `json:"version"`
	Proposal ArtifactRef `json:"proposal"`
	Base     ArtifactRef `json:"base"`
	Revision int64       `json:"revision"`
	Accepted []string    `json:"accepted_candidate_ids"`
	Rejected []string    `json:"rejected_candidate_ids"`
}
type DustPlan struct {
	Revision     int64
	Route        string
	Selection    DustSelection
	DecisionHash string
}

func (p VerifiedDustProposal) Bind(s DustSnapshot) (DustSnapshot, error) {
	if p.ref != s.Proposal {
		return DustSnapshot{}, errors.New("dust proposal binding mismatch")
	}
	s.CandidateIDs = make([]string, len(p.document.Candidates))
	for i, c := range p.document.Candidates {
		s.CandidateIDs[i] = c.ID
	}
	return s, nil
}
func PlanDust(s DustSnapshot, actor uuid.UUID, r Decision) (DustPlan, error) {
	fail := func(code string, status int) (DustPlan, error) { return DustPlan{}, domain.NewError(code, status) }
	if actor == uuid.Nil || s.TenantID == uuid.Nil || s.ReviewID == uuid.Nil || s.RunID == uuid.Nil || s.Revision < 1 || s.Revision >= maxRevision || s.State != "pending" || !s.Proposal.valid() || !s.Base.valid() {
		return fail("INTERNAL_ERROR", 500)
	}
	if r.ExpectedRevision != s.Revision || r.CandidateHash != s.Proposal.SHA256 {
		return fail("REVISION_CONFLICT", 409)
	}
	if r.Decision != "accept" && r.Decision != "reject" && r.Decision != "skip" || r.Decision != "accept" && (len(r.Accepted) > 0 || len(r.Rejected) > 0) {
		return fail("VALIDATION_FAILED", 422)
	}
	known := map[string]bool{}
	for _, id := range s.CandidateIDs {
		known[id] = true
	}
	seen := map[string]bool{}
	for _, set := range [][]string{r.Accepted, r.Rejected} {
		for _, id := range set {
			if seen[id] || !known[id] {
				return fail("UNKNOWN_DEFECT_CANDIDATE", 422)
			}
			seen[id] = true
		}
	}
	selection := DustSelection{"sensor_dust_review_v1", s.Proposal, s.Base, s.Revision + 1, append([]string{}, r.Accepted...), append([]string{}, r.Rejected...)}
	sort.Strings(selection.Accepted)
	sort.Strings(selection.Rejected)
	route := "base"
	if r.Decision == "skip" {
		route = "skip"
	} else if len(selection.Accepted) > 0 {
		route = "repair"
	}
	hash, err := canonical.Hash(struct {
		Version   string        `json:"version"`
		Tenant    uuid.UUID     `json:"tenant_id"`
		Review    uuid.UUID     `json:"review_id"`
		Run       uuid.UUID     `json:"run_id"`
		Actor     uuid.UUID     `json:"actor_id"`
		Decision  string        `json:"decision"`
		Selection DustSelection `json:"selection"`
	}{"sensor_dust_decision_v1", s.TenantID, s.ReviewID, s.RunID, actor, r.Decision, selection})
	if err != nil {
		return DustPlan{}, err
	}
	return DustPlan{selection.Revision, route, selection, hash}, nil
}
