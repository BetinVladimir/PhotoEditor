package workflow

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Transport retains the exact original control bytes for subsequent SQL
// witness/admission checks. Publication alone never authorizes dispatch.
type PerspectiveLinesClosure struct {
	task      uuid.UUID
	entries   []v2input.Entry
	built     v2input.Built
	documents map[uuid.UUID][]byte
}

func (c PerspectiveLinesClosure) Entries() []v2input.Entry {
	return append([]v2input.Entry(nil), c.entries...)
}
func (c PerspectiveLinesClosure) Built() v2input.Built {
	b := c.built
	b.Chunks = append([]v2input.Ref(nil), b.Chunks...)
	return b
}
func (c PerspectiveLinesClosure) Documents() map[uuid.UUID][]byte {
	result := make(map[uuid.UUID][]byte, len(c.documents))
	for id, raw := range c.documents {
		result[id] = bytes.Clone(raw)
	}
	return result
}

type perspectiveLinesCursor struct {
	entries []v2input.Entry
	offset  int
}

func (c *perspectiveLinesCursor) Next(ctx context.Context) (v2input.Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Entry{}, false, err
	}
	if c.offset == len(c.entries) {
		return v2input.Entry{}, false, nil
	}
	e := c.entries[c.offset]
	c.offset++
	return e, true, nil
}

type perspectiveLinesClosurePublisher struct {
	out       v2input.Publisher
	task      uuid.UUID
	seen      map[uuid.UUID]bool
	documents map[uuid.UUID][]byte
}

func (p *perspectiveLinesClosurePublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, raw []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if task != p.task || ordinal < 0 || ordinal > 1 || (contract != "InputClosureChunk" && contract != "InputClosureManifest") {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	original := bytes.Clone(raw)
	ref, err := p.out.Publish(ctx, task, ordinal, contract, bytes.Clone(raw))
	if err != nil {
		return v2input.Ref{}, err
	}
	if ref.ArtifactID == uuid.Nil || ref.SHA256 != renderDerivativeBytesSHA(original) || p.seen[ref.ArtifactID] {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	p.seen[ref.ArtifactID] = true
	p.documents[ref.ArtifactID] = original
	return ref, nil
}
func BuildPerspectiveLinesClosure(ctx context.Context, plan PerspectiveLinesPlan, params review.ArtifactRef, out v2input.Publisher) (PerspectiveLinesClosure, error) {
	var zero PerspectiveLinesClosure
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if out == nil {
		return zero, v2input.ErrInvalid
	}
	entries, err := plan.Entries(params)
	if err != nil {
		return zero, err
	}
	// At most128 historical dense transforms plus original/pixels/alpha/Params.
	// One bounded chunk and one manifest fit the reserved control-document cap.
	if len(entries) < 3 || len(entries) > 132 {
		return zero, v2input.ErrInvalid
	}
	publisher := &perspectiveLinesClosurePublisher{out: out, task: plan.task, seen: map[uuid.UUID]bool{}, documents: map[uuid.UUID][]byte{}}
	for _, entry := range entries {
		publisher.seen[entry.Ref.ArtifactID] = true
	}
	built, err := v2input.BuildInputClosure(ctx, plan.task, &perspectiveLinesCursor{entries: entries}, publisher)
	if err != nil {
		return zero, err
	}
	if built.Count != len(entries) || len(built.Chunks) != 1 || len(publisher.documents) != 2 {
		return zero, v2input.ErrInvalid
	}
	return PerspectiveLinesClosure{task: plan.task, entries: entries, built: built, documents: publisher.documents}, nil
}
