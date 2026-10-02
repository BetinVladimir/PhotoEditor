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
	"regexp"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

type Canvas struct {
	Width        int64  `json:"width"`
	Height       int64  `json:"height"`
	Orientation  int    `json:"orientation"`
	SpaceID      string `json:"space_id"`
	SourceSHA256 string `json:"source_sha256"`
}
type rectangle struct {
	X      int64 `json:"x"`
	Y      int64 `json:"y"`
	Width  int64 `json:"width"`
	Height int64 `json:"height"`
}
type candidate struct {
	ID       string    `json:"id"`
	Label    int       `json:"label"`
	BBox     rectangle `json:"bbox"`
	Diameter float64   `json:"diameter"`
	Score    float64   `json:"score"`
	Decision string    `json:"decision"`
}
type proposalDocument struct {
	Version        string          `json:"version"`
	Image          ArtifactRef     `json:"image"`
	SourcePixels   ArtifactRef     `json:"source_pixels"`
	LabelMap       ArtifactRef     `json:"label_map"`
	Canvas         Canvas          `json:"canvas"`
	TransformChain json.RawMessage `json:"transform_chain"`
	Candidates     []candidate     `json:"candidates"`
}

// DefectSource is obtained from the verified input WorkingImage, not from the
// review request. Its transform chain must already have passed image validation.
type DefectSource struct {
	Image, Pixels  ArtifactRef
	Canvas         Canvas
	TransformChain json.RawMessage
}

// VerifiedProposal has no public mutable candidate catalog. It only attests
// bytes/identity/geometry; the artifact repository must separately check tenant,
// readiness and provenance from the successful proposal task.
type VerifiedProposal struct {
	ref      ArtifactRef
	document proposalDocument
}

func (p VerifiedProposal) Bind(s DefectSnapshot) (DefectSnapshot, error) {
	if !p.ref.valid() || s.Proposal != p.ref || s.Image != p.document.Image || s.LabelMap != p.document.LabelMap {
		return DefectSnapshot{}, errors.New("proposal snapshot binding mismatch")
	}
	s.CandidateIDs = make([]string, len(p.document.Candidates))
	for i, c := range p.document.Candidates {
		s.CandidateIDs[i] = c.ID
	}
	return s, nil
}

var spaceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$`)

// ReadDefectProposal reads at most 1MiB+1, verifies SHA of the original bytes,
// and rederives every candidate ID using the worker's RFC8785 contract. Caller
// supplies a context-aware object reader for cancellation during network reads.
func ReadDefectProposal(ctx context.Context, reader io.Reader, ref ArtifactRef, source DefectSource) (VerifiedProposal, error) {
	fail := func() (VerifiedProposal, error) { return VerifiedProposal{}, errors.New("invalid defect proposal") }
	if err := ctx.Err(); err != nil {
		return VerifiedProposal{}, err
	}
	if !ref.valid() || !source.Image.valid() || !source.Pixels.valid() {
		return fail()
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 1048577))
	if err != nil {
		return VerifiedProposal{}, err
	}
	if err = ctx.Err(); err != nil {
		return VerifiedProposal{}, err
	}
	if len(raw) > 1048576 {
		return fail()
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return fail()
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return fail()
	}
	var doc proposalDocument
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&doc); err != nil {
		return fail()
	}
	// Require every wire field, reject null primitive coercion and missing arrays.
	roundtrip, err := canonical.Marshal(doc)
	if err != nil || !bytes.Equal(normalized, roundtrip) {
		return fail()
	}
	if doc.Version != "defect_candidates_v1" || doc.Image != source.Image || doc.SourcePixels != source.Pixels || !doc.LabelMap.valid() ||
		doc.Canvas != source.Canvas || doc.Candidates == nil || len(doc.Candidates) > 4096 {
		return fail()
	}
	canvas := doc.Canvas
	if canvas.Width < 1 || canvas.Width > 2147483647 || canvas.Height < 1 || canvas.Height > 2147483647 || canvas.Width*canvas.Height > 200000000 || canvas.Orientation != 1 || !spaceIDPattern.MatchString(canvas.SpaceID) {
		return fail()
	}
	if _, err = domain.ParseDigest(canvas.SourceSHA256); err != nil {
		return fail()
	}
	expectedChain, err := canonical.Bytes(source.TransformChain)
	if err != nil {
		return fail()
	}
	actualChain, err := canonical.Bytes(doc.TransformChain)
	if err != nil || !bytes.Equal(expectedChain, actualChain) || len(actualChain) == 0 || actualChain[0] != '{' {
		return fail()
	}
	ids, labels := map[string]bool{}, map[int]bool{}
	for _, c := range doc.Candidates {
		if err = ctx.Err(); err != nil {
			return VerifiedProposal{}, err
		}
		b := c.BBox
		if c.Label < 1 || c.Label > 65535 || labels[c.Label] || ids[c.ID] || c.Decision != "pending" || math.IsNaN(c.Diameter) || math.IsInf(c.Diameter, 0) ||
			c.Diameter <= 0 || c.Diameter > 200000000 || c.Score < 0 || c.Score > 1 || b.X < 0 || b.Y < 0 || b.Width < 1 || b.Height < 1 || b.X >= canvas.Width || b.Y >= canvas.Height || b.Width > canvas.Width-b.X || b.Height > canvas.Height-b.Y {
			return fail()
		}
		id, e := canonical.Hash(struct {
			Version  string    `json:"version"`
			Source   string    `json:"source_sha256"`
			Mask     string    `json:"mask_sha256"`
			Label    int       `json:"label"`
			BBox     rectangle `json:"bbox"`
			Diameter float64   `json:"diameter"`
		}{"defect_candidate_v1", doc.SourcePixels.SHA256, doc.LabelMap.SHA256, c.Label, c.BBox, c.Diameter})
		if e != nil || c.ID != id {
			return fail()
		}
		ids[c.ID] = true
		labels[c.Label] = true
	}
	return VerifiedProposal{ref: ref, document: doc}, nil
}
