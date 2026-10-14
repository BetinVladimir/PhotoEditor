package review

import (
	"context"
	"io"
	"photoeditor/backend/internal/masknpy"
)

// VerifyLabelMap validates physical labels against this immutable verified
// proposal. Caller supplies its catalog-authorized stream and closes it.
// Pixel presence never changes any candidate decision.
func (p VerifiedProposal) VerifyLabelMap(ctx context.Context, reader io.Reader, objectBytes int64) ([]masknpy.LabelEvidence, error) {
	if !p.ref.valid() || !p.document.LabelMap.valid() {
		return nil, masknpy.ErrInvalid
	}
	bounds := make([]masknpy.LabelBounds, len(p.document.Candidates))
	for i, c := range p.document.Candidates {
		bounds[i] = masknpy.LabelBounds{Label: uint16(c.Label), X: c.BBox.X, Y: c.BBox.Y, Width: c.BBox.Width, Height: c.BBox.Height}
	}
	return masknpy.VerifyDefectLabelsNPY(ctx, masknpy.PinnedMask{Reader: reader, Bytes: objectBytes, SHA256: p.document.LabelMap.SHA256}, p.document.Canvas.Width, p.document.Canvas.Height, bounds)
}

// VerifyProtectedLabelMap checks the complete candidate inventory and positive
// protection exclusion together. Both original streams remain caller-owned.
func (p VerifiedProposal) VerifyProtectedLabelMap(ctx context.Context, labels masknpy.PinnedMask, protection masknpy.PinnedMask) ([]masknpy.LabelEvidence, error) {
	if !p.ref.valid() || !p.document.LabelMap.valid() || labels.SHA256 != p.document.LabelMap.SHA256 {
		return nil, masknpy.ErrInvalid
	}
	bounds := make([]masknpy.LabelBounds, len(p.document.Candidates))
	for i, c := range p.document.Candidates {
		bounds[i] = masknpy.LabelBounds{Label: uint16(c.Label), X: c.BBox.X, Y: c.BBox.Y, Width: c.BBox.Width, Height: c.BBox.Height}
	}
	return masknpy.VerifyDefectLabelsProtectionNPY(ctx, labels, protection, p.document.Canvas.Width, p.document.Canvas.Height, bounds)
}
