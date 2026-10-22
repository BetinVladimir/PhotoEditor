package semanticproposal

import (
	"context"
	"errors"
	"io"
	"photoeditor/backend/internal/objectstore"
	"reflect"
	"slices"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// A phase belongs to one synchronous, pure completed-read operation. Its reader
// copy never escapes; publication and registration use the original reader.
// Entries contain immutable byte-derived results, never authorization decisions.
const nativeReadPhaseLimit = 16

type nativeReadKey struct{ stage, seal string }
type nativeReadEntry struct {
	value    any
	validate func(context.Context) error
}
type nativeReadPhase struct {
	reader      SQLTransferSourceReader
	who         auth.Identity
	color       TransferColorProfile
	entries     map[nativeReadKey]nativeReadEntry
	order       []nativeReadKey
	running     map[nativeReadKey]bool
	failed      error
	closed      bool
	objects     map[objectstore.Object]bool
	objectOrder []objectstore.Object
	fencing     bool
	tracker     *nativePhaseObjects
	sourceScene *createScenePolicy
}

func (p *nativeReadPhase) fail(err error) error {
	if p.failed == nil {
		p.failed = err
	}
	return p.failed
}

func (p *nativeReadPhase) matches(s SQLTransferSourceReader, who auth.Identity, color TransferColorProfile) bool {
	if p.closed || who != p.who || color != p.color || s.DB != p.reader.DB || s.Objects != p.tracker || s.manualRasterBudget != p.reader.manualRasterBudget {
		return false
	}
	if !reflect.DeepEqual(s.scenePolicy, p.reader.scenePolicy) {
		return false
	}
	s.scenePolicy = p.reader.scenePolicy
	s.nativePhase = nil
	s.Objects = p.reader.Objects
	return reflect.DeepEqual(s, p.reader)
}

func (p *nativeReadPhase) close() {
	p.closed = true
	p.entries = nil
	p.running = nil
	p.order = nil
	p.objects = nil
	p.objectOrder = nil
}

func (p *nativeReadPhase) finish(ctx context.Context) error {
	defer p.close()
	if p.failed != nil {
		return p.failed
	}
	if !reflect.DeepEqual(p.sourceScene, p.reader.scenePolicy) {
		return p.fail(v2input.ErrConflict)
	}
	p.fencing = true
	// An immutable version is still hash-verified again: a broken object store or
	// injected reader must not change bytes under otherwise unchanged SQL pins.
	for _, obj := range p.objectOrder {
		if err := verifyNativeReadObject(ctx, p.reader.Objects, obj); err != nil {
			return p.fail(err)
		}
	}
	for _, key := range p.order {
		if err := ctx.Err(); err != nil {
			return p.fail(err)
		}
		if err := p.entries[key].validate(ctx); err != nil {
			return p.fail(err)
		}
		if p.failed != nil {
			return p.failed
		}
	}
	if err := ctx.Err(); err != nil {
		return p.fail(err)
	}
	if !reflect.DeepEqual(p.sourceScene, p.reader.scenePolicy) {
		return p.fail(v2input.ErrConflict)
	}
	return nil
}

// beginNativeReadScope owns only one synchronous completed-read operation.
// No authorization decisions are memoized; finish rechecks bytes and witnesses.
func beginNativeReadScope(s SQLTransferSourceReader, who auth.Identity, color TransferColorProfile) SQLTransferSourceReader {
	original := s
	original.scenePolicy = cloneNativeScenePolicy(s.scenePolicy)
	s.nativePhase = &nativeReadPhase{reader: original, who: who, color: color, entries: map[nativeReadKey]nativeReadEntry{}, running: map[nativeReadKey]bool{}, objects: map[objectstore.Object]bool{}, sourceScene: s.scenePolicy}
	s.scenePolicy = cloneNativeScenePolicy(original.scenePolicy)
	s.nativePhase.tracker = &nativePhaseObjects{base: original.Objects, phase: s.nativePhase}
	s.Objects = s.nativePhase.tracker
	return s
}

// nativeReadOperation shares immutable typed results across sibling reads.
// Every dependency remains fenced before the owning operation returns.
func nativeReadOperation(ctx context.Context, s SQLTransferSourceReader, who auth.Identity, color TransferColorProfile, read func(SQLTransferSourceReader) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	owner := s.nativePhase == nil
	if owner {
		s = beginNativeReadScope(s, who, color)
	}
	p := s.nativePhase
	if owner {
		defer p.close()
	}
	if !p.matches(s, who, color) {
		return p.fail(v2input.ErrConflict)
	}
	if p.failed != nil {
		return p.failed
	}
	if err := read(s); err != nil {
		return p.fail(err)
	}
	if p.failed != nil {
		return p.failed
	}
	if owner {
		return p.finish(ctx)
	}
	return ctx.Err()
}

// The type parameter is private to the five typed wrappers. A cached proof is
// detached on both sides; even internal consumers cannot mutate its witness.
func nativeRead[T any](ctx context.Context, s SQLTransferSourceReader, who auth.Identity, color TransferColorProfile, key nativeReadKey, clone func(T) T, build func(SQLTransferSourceReader) (T, error), witness func(SQLTransferSourceReader, T) func(context.Context) error) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	owner := s.nativePhase == nil
	if owner {
		s = beginNativeReadScope(s, who, color)
	}
	p := s.nativePhase
	if owner {
		defer p.close()
	}
	if !p.matches(s, who, color) || key.seal == "" {
		return zero, p.fail(v2input.ErrConflict)
	}
	if p.failed != nil {
		return zero, p.failed
	}
	if entry, ok := p.entries[key]; ok {
		value, ok := entry.value.(T)
		if !ok {
			return zero, p.fail(v2input.ErrConflict)
		}
		return clone(value), nil
	}
	if p.running[key] || len(p.entries)+len(p.running) >= nativeReadPhaseLimit {
		return zero, p.fail(v2input.ErrConflict)
	}
	p.running[key] = true
	value, err := build(s)
	delete(p.running, key)
	if err != nil {
		p.fail(err)
		if owner {
			_ = p.finish(ctx)
		}
		return zero, err
	}
	if p.failed != nil {
		if owner {
			_ = p.finish(ctx)
		}
		return zero, p.failed
	}
	retained := clone(value)
	p.entries[key] = nativeReadEntry{value: retained, validate: witness(s, clone(retained))}
	p.order = append(p.order, key)
	if owner {
		if err := p.finish(ctx); err != nil {
			return zero, err
		}
	}
	return clone(retained), nil
}

// Read-only tracker records exact original locators; no body cache or authority
// cache exists. All normal per-format readers still enforce their own bounds.
type nativePhaseObjects struct {
	base  v2input.SessionAnalysisObjectReader
	phase *nativeReadPhase
}

func (r *nativePhaseObjects) Open(ctx context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	p := r.phase
	if p.closed || p.failed != nil {
		return nil, v2input.ErrConflict
	}
	if !p.objects[obj] {
		if p.fencing || len(p.objects) >= 1024 {
			return nil, p.fail(v2input.ErrConflict)
		}
		p.objects[obj] = true
		p.objectOrder = append(p.objectOrder, obj)
	}
	if err := ctx.Err(); err != nil {
		return nil, p.fail(err)
	}
	if r.base == nil {
		return nil, p.fail(v2input.ErrInvalid)
	}
	stream, err := r.base.Open(ctx, obj)
	if err != nil {
		var closeErr error
		if stream != nil {
			closeErr = stream.Close()
		}
		return nil, p.fail(errors.Join(err, closeErr, ctx.Err()))
	}
	if stream == nil {
		return nil, p.fail(v2input.ErrInvalid)
	}
	return stream, nil
}

func cloneNativeScenePolicy(p *createScenePolicy) *createScenePolicy {
	if p == nil {
		return nil
	}
	out := *p
	out.Policy = slices.Clone(p.Policy)
	cloneExpected := func(v createExpectation) createExpectation {
		v.RequiredChecks = slices.Clone(v.RequiredChecks)
		v.CalibrationSHAs = slices.Clone(v.CalibrationSHAs)
		v.TestFixtureSHAs = slices.Clone(v.TestFixtureSHAs)
		v.EvidenceSHAs = slices.Clone(v.EvidenceSHAs)
		return v
	}
	out.Scene.Expected = cloneExpected(p.Scene.Expected)
	out.Depth.Expected = cloneExpected(p.Depth.Expected)
	return &out
}
