package review

import (
 "bytes"
 "context"
)

// ParseDefectProposalStructure checks the complete immutable document and its
// internal geometry. It does not attest an actual source image, producer,
// review decision, or physical label bytes. Callers must independently bind
// those through ReadDefectProposal and the current scoped authority.
func ParseDefectProposalStructure(ctx context.Context, raw []byte, ref ArtifactRef) (VerifiedProposal, error) {
 if err := ctx.Err(); err != nil { return VerifiedProposal{}, err }
 var doc proposalDocument
 if err := decodeExact(raw, &doc); err != nil { return VerifiedProposal{}, err }
 if err := validateTransformChain(doc.TransformChain, doc.Canvas); err != nil { return VerifiedProposal{}, err }
 source := DefectSource{Image:doc.Image, Pixels:doc.SourcePixels, Canvas:doc.Canvas, TransformChain:doc.TransformChain}
 // Use the same candidate IDs, bounds, closed fields and original-byte hash
 // rules as source-bound proposal reading, without inventing another schema.
 return ReadDefectProposal(ctx, bytes.NewReader(raw), ref, source)
}

