package semanticproposal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type phaseProof struct{ values []int }

func clonePhaseProof(p phaseProof) phaseProof { return phaseProof{append([]int(nil), p.values...)} }
func phaseTestIdentity() auth.Identity {
	return auth.Identity{UserID: uuid.New(), SessionID: uuid.New(), Epoch: 1}
}
func phaseTestWitness(SQLTransferSourceReader, phaseProof) func(context.Context) error {
	return func(context.Context) error { return nil }
}

func TestNativeReadPhaseDiamondDeduplicatesAndDetaches(t *testing.T) {
	ctx := context.Background()
	who := phaseTestIdentity()
	s := SQLTransferSourceReader{}
	count, checks := 0, 0
	var retained SQLTransferSourceReader
	leaf := func(r SQLTransferSourceReader) (phaseProof, error) {
		return nativeRead(ctx, r, who, TransferColorProfile{}, nativeReadKey{"detector", "seal"}, clonePhaseProof, func(SQLTransferSourceReader) (phaseProof, error) { count++; return phaseProof{[]int{7}}, nil }, func(SQLTransferSourceReader, phaseProof) func(context.Context) error {
			return func(context.Context) error { checks++; return nil }
		})
	}
	out, err := nativeRead(ctx, s, who, TransferColorProfile{}, nativeReadKey{"defect", "root"}, clonePhaseProof, func(r SQLTransferSourceReader) (phaseProof, error) {
		retained = r
		first, err := leaf(r)
		if err != nil {
			return phaseProof{}, err
		}
		first.values[0] = 99
		second, err := leaf(r)
		if err != nil {
			return phaseProof{}, err
		}
		if second.values[0] != 7 {
			t.Fatal("memo aliases consumer")
		}
		return second, nil
	}, phaseTestWitness)
	if err != nil || count != 1 || checks != 1 || out.values[0] != 7 {
		t.Fatalf("out=%v build=%d checks=%d err=%v", out, count, checks, err)
	}
	if _, err := leaf(retained); err == nil {
		t.Fatal("reader survived closed phase")
	}
	// A new independent call cannot reuse any proof or authority from this phase.
	if _, err := leaf(s); err != nil || count != 2 || checks != 2 {
		t.Fatalf("fresh scope: %d/%d %v", count, checks, err)
	}
}

func TestNativeReadPhaseRejectsChangedTransitiveWitness(t *testing.T) {
	for _, name := range []string{"descendant_result", "policy", "locator", "actor", "session"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			who := phaseTestIdentity()
			live := true
			checks := 0
			got, err := nativeRead(ctx, SQLTransferSourceReader{}, who, TransferColorProfile{}, nativeReadKey{"defect", "root"}, clonePhaseProof, func(r SQLTransferSourceReader) (phaseProof, error) {
				_, err := nativeRead(ctx, r, who, TransferColorProfile{}, nativeReadKey{"detector", "child"}, clonePhaseProof, func(SQLTransferSourceReader) (phaseProof, error) { return phaseProof{[]int{1}}, nil }, func(SQLTransferSourceReader, phaseProof) func(context.Context) error {
					return func(context.Context) error {
						checks++
						if !live {
							return errors.New(name + " revoked")
						}
						return nil
					}
				})
				live = false
				return phaseProof{[]int{2}}, err
			}, phaseTestWitness)
			if err == nil || got.values != nil || checks != 1 {
				t.Fatalf("leaked proof: %v %v checks=%d", got, err, checks)
			}
		})
	}
}

func TestNativeReadPhaseRejectsForeignScopeAndStickyFailure(t *testing.T) {
	for _, name := range []string{"actor", "session", "epoch", "color", "reader", "empty_seal"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			who := phaseTestIdentity()
			color := TransferColorProfile{sha: "original"}
			out, err := nativeRead(ctx, SQLTransferSourceReader{}, who, color, nativeReadKey{"root", "root"}, clonePhaseProof, func(r SQLTransferSourceReader) (phaseProof, error) {
				other := who
				otherColor := color
				key := nativeReadKey{"child", "seal"}
				switch name {
				case "actor":
					other.UserID = uuid.New()
				case "session":
					other.SessionID = uuid.New()
				case "epoch":
					other.Epoch++
				case "color":
					otherColor.sha = "changed"
				case "reader":
					r.ProjectID = uuid.New()
				case "empty_seal":
					key.seal = ""
				}
				_, _ = nativeRead(ctx, r, other, otherColor, key, clonePhaseProof, func(SQLTransferSourceReader) (phaseProof, error) {
					t.Fatal("foreign builder ran")
					return phaseProof{}, nil
				}, phaseTestWitness)
				// Ignoring a failed descendant must not make its ancestor usable.
				return phaseProof{[]int{42}}, nil
			}, phaseTestWitness)
			if err == nil || out.values != nil {
				t.Fatalf("scope escaped: %v %v", out, err)
			}
		})
	}
}

func TestNativeReadPhaseSeparatesPlanSealsAndStages(t *testing.T) {
	ctx := context.Background()
	who := phaseTestIdentity()
	count := 0
	_, err := nativeRead(ctx, SQLTransferSourceReader{}, who, TransferColorProfile{}, nativeReadKey{"root", "root"}, clonePhaseProof, func(r SQLTransferSourceReader) (phaseProof, error) {
		for _, key := range []nativeReadKey{{"parse", "one"}, {"parse", "two"}, {"landmark", "one"}} {
			if _, err := nativeRead(ctx, r, who, TransferColorProfile{}, key, clonePhaseProof, func(SQLTransferSourceReader) (phaseProof, error) { count++; return phaseProof{}, nil }, phaseTestWitness); err != nil {
				return phaseProof{}, err
			}
		}
		return phaseProof{}, nil
	}, phaseTestWitness)
	if err != nil || count != 3 {
		t.Fatalf("keys collided: %d %v", count, err)
	}
}

func TestNativeReadPhaseCyclesBoundsCancellationAndNoErrorMemo(t *testing.T) {
	for _, mode := range []string{"cycle", "bound", "cancel", "failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			who := phaseTestIdentity()
			key := nativeReadKey{"root", "seal"}
			out, err := nativeRead(ctx, SQLTransferSourceReader{}, who, TransferColorProfile{}, key, clonePhaseProof, func(r SQLTransferSourceReader) (phaseProof, error) {
				switch mode {
				case "cycle":
					_, _ = nativeRead(ctx, r, who, TransferColorProfile{}, key, clonePhaseProof, func(SQLTransferSourceReader) (phaseProof, error) { t.Fatal("cycle entered"); return phaseProof{}, nil }, phaseTestWitness)
				case "bound":
					for i := 0; i < nativeReadPhaseLimit; i++ {
						_, _ = nativeRead(ctx, r, who, TransferColorProfile{}, nativeReadKey{"child", fmt.Sprint(i)}, clonePhaseProof, func(SQLTransferSourceReader) (phaseProof, error) { return phaseProof{}, nil }, phaseTestWitness)
					}
				case "cancel":
					cancel()
				case "failure":
					_, _ = nativeRead(ctx, r, who, TransferColorProfile{}, nativeReadKey{"child", "failure"}, clonePhaseProof, func(SQLTransferSourceReader) (phaseProof, error) {
						return phaseProof{[]int{3}}, errors.New("failed byte verification")
					}, phaseTestWitness)
				}
				return phaseProof{[]int{7}}, nil
			}, phaseTestWitness)
			if err == nil || out.values != nil {
				t.Fatalf("partial proof escaped: %v %v", out, err)
			}
		})
	}
}

type phaseMemoryObjects struct {
	body  string
	opens int
}

func (o *phaseMemoryObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	o.opens++
	return io.NopCloser(strings.NewReader(o.body)), nil
}
func TestNativeReadPhaseRejectsPhysicalMutationUnderSameLocator(t *testing.T) {
	ctx := context.Background()
	who := phaseTestIdentity()
	objects := &phaseMemoryObjects{body: "{}"}
	obj := objectstore.Object{Key: "original/key", VersionID: "v1", ContentType: "application/json", Bytes: 2, SHA256: "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"}
	out, err := nativeRead(ctx, SQLTransferSourceReader{Objects: objects}, who, TransferColorProfile{}, nativeReadKey{"root", "seal"}, clonePhaseProof, func(r SQLTransferSourceReader) (phaseProof, error) {
		if _, err := v2input.ReadPinnedSemanticDocument(ctx, r.Objects, obj, v2input.Ref{ArtifactID: uuid.New(), SHA256: obj.SHA256}); err != nil {
			return phaseProof{}, err
		}
		objects.body = "[]" // Same size and every catalog/locator field unchanged.
		return phaseProof{[]int{9}}, nil
	}, phaseTestWitness)
	if err == nil || out.values != nil || objects.opens != 2 {
		t.Fatalf("corrupt original accepted: %v %v opens=%d", out, err, objects.opens)
	}
}

type phaseOpenFailure struct {
	stream io.ReadCloser
	err    error
}

func (o *phaseOpenFailure) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	return o.stream, o.err
}

type phaseCloseProbe struct {
	closed int
	err    error
}

func (p *phaseCloseProbe) Read([]byte) (int, error) { return 0, io.EOF }
func (p *phaseCloseProbe) Close() error             { p.closed++; return p.err }
func TestNativeReadPhaseTrackerClosesFailedOpenAndRejectsNil(t *testing.T) {
	openErr, closeErr := errors.New("open"), errors.New("close")
	for _, mode := range []string{"stream_error", "nil_stream"} {
		t.Run(mode, func(t *testing.T) {
			stream := &phaseCloseProbe{err: closeErr}
			base := &phaseOpenFailure{}
			if mode == "stream_error" {
				base.stream = stream
				base.err = openErr
			}
			who := phaseTestIdentity()
			got, err := nativeRead(context.Background(), SQLTransferSourceReader{Objects: base}, who, TransferColorProfile{}, nativeReadKey{"root", "seal"}, clonePhaseProof, func(r SQLTransferSourceReader) (phaseProof, error) {
				_, _ = r.Objects.Open(context.Background(), objectstore.Object{Key: "key"})
				return phaseProof{[]int{1}}, nil
			}, phaseTestWitness)
			if err == nil || got.values != nil {
				t.Fatalf("failed Open escaped: %v %v", got, err)
			}
			if mode == "stream_error" && (stream.closed != 1 || !errors.Is(err, openErr) || !errors.Is(err, closeErr)) {
				t.Fatalf("close/errors lost: %d %v", stream.closed, err)
			}
		})
	}
}
func TestNativeReadPhaseRejectsParseLocatorEvenIfOtherVersionTracked(t *testing.T) {
	ctx := context.Background()
	who := phaseTestIdentity()
	objects := &phaseMemoryObjects{body: "{}"}
	original := defectEvidenceObject{ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"}, object: objectstore.Object{Key: "labels", VersionID: "v1", ContentType: "application/json", Bytes: 2, SHA256: "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"}}
	other := original
	other.object.VersionID = "v2"
	current := original
	got, err := nativeRead(ctx, SQLTransferSourceReader{Objects: objects}, who, TransferColorProfile{}, nativeReadKey{"parse", "seal"}, clonePhaseProof, func(r SQLTransferSourceReader) (phaseProof, error) {
		for _, pin := range []defectEvidenceObject{original, other} {
			stream, err := r.Objects.Open(ctx, pin.object)
			if err != nil {
				return phaseProof{}, err
			}
			if err := stream.Close(); err != nil {
				return phaseProof{}, err
			}
		}
		current = other
		return phaseProof{[]int{1}}, nil
	}, func(SQLTransferSourceReader, phaseProof) func(context.Context) error {
		return func(context.Context) error { return verifyNativeParsePixelPin(original, current) }
	})
	if err == nil || got.values != nil || objects.opens != 4 {
		t.Fatalf("tracked union replaced exact parse pin: %v %v opens=%d", got, err, objects.opens)
	}
}

func TestNativeReadTypedProofClonesOwnAllMutableFields(t *testing.T) {
	detector := completedNativeDetector{boxes: review.FaceBoxes{Faces: []review.FaceBox{{Confidence: 0.5}}, Warnings: []string{"original"}}}
	landmarks := completedNativeLandmarks{catalog: nativeFaceResultCatalog{Outputs: []v2input.Ref{{SHA256: "original"}}}, detector: detector, faces: []review.Face68{{Points: []review.Landmark{{X: 0.5}}, TransformChain: []byte("[]")}}}
	parse := completedNativeFaceParse{catalog: landmarks.catalog, detector: detector, labels: review.FaceLabels19{TransformChain: []byte("[]"), Warnings: []string{"original"}}}
	id := uuid.New()
	model := "original"
	mask := review.ProtectionMask{ObjectID: &id, ModelLockID: &model, TransformChain: []byte("[]")}
	skin := completedNativeSkin{catalog: nativeSkinResultCatalog{Outputs: landmarks.catalog.Outputs}, parser: parse, mask: mask}
	defect := completedNativeInputDefect{catalog: nativeInputDefectResultCatalog{Outputs: landmarks.catalog.Outputs}, evidence: VerifiedDefectEvidence{Params: []byte("{}"), Proposal: []byte("{}"), Report: []byte("{}"), FaceMasks: []review.ProposalFaceMask{{ObjectID: &id, Mask: mask}}}}
	d := cloneNativeDetector(detector)
	d.boxes.Faces[0].Confidence = 1
	d.boxes.Warnings[0] = "changed"
	l := cloneNativeLandmarks(landmarks)
	l.catalog.Outputs[0].SHA256 = "changed"
	l.faces[0].Points[0].X = 1
	l.faces[0].TransformChain[0] = 'x'
	l.detector.boxes.Warnings[0] = "changed"
	p := cloneNativeParse(parse)
	p.catalog.Outputs[0].SHA256 = "changed"
	p.labels.Warnings[0] = "changed"
	p.labels.TransformChain[0] = 'x'
	p.detector.boxes.Faces[0].Confidence = 1
	k := cloneNativeSkin(skin)
	k.catalog.Outputs[0].SHA256 = "changed"
	*k.mask.ObjectID = uuid.Nil
	*k.mask.ModelLockID = "changed"
	k.mask.TransformChain[0] = 'x'
	k.parser.labels.Warnings[0] = "changed"
	f := cloneNativeDefect(defect)
	f.catalog.Outputs[0].SHA256 = "changed"
	f.evidence.Params[0] = 'x'
	f.evidence.Proposal[0] = 'x'
	f.evidence.Report[0] = 'x'
	*f.evidence.FaceMasks[0].ObjectID = uuid.Nil
	*f.evidence.FaceMasks[0].Mask.ModelLockID = "changed"
	f.evidence.FaceMasks[0].Mask.TransformChain[0] = 'x'
	if detector.boxes.Faces[0].Confidence != 0.5 || detector.boxes.Warnings[0] != "original" || landmarks.catalog.Outputs[0].SHA256 != "original" || landmarks.faces[0].Points[0].X != 0.5 || string(landmarks.faces[0].TransformChain) != "[]" || parse.labels.Warnings[0] != "original" || string(parse.labels.TransformChain) != "[]" || *skin.mask.ObjectID != id || *skin.mask.ModelLockID != "original" || string(skin.mask.TransformChain) != "[]" || string(defect.evidence.Params) != "{}" || string(defect.evidence.Proposal) != "{}" || string(defect.evidence.Report) != "{}" || *defect.evidence.FaceMasks[0].ObjectID != id || string(defect.evidence.FaceMasks[0].Mask.TransformChain) != "[]" {
		t.Fatal("typed cached proof aliases consumer")
	}
}

func TestNativeReadPhaseScenePolicyOwnedValueIdentity(t *testing.T) {
	for _, mode := range []string{"identical_copy", "policy_alias", "expectation_alias", "changed_copy"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			who := phaseTestIdentity()
			policy := &createScenePolicy{Policy: []byte("{}"), Scene: createSceneProducer{Expected: createExpectation{RequiredChecks: []string{"original"}}}}
			out, err := nativeRead(ctx, SQLTransferSourceReader{scenePolicy: policy}, who, TransferColorProfile{}, nativeReadKey{"root", "seal"}, clonePhaseProof, func(r SQLTransferSourceReader) (phaseProof, error) {
				switch mode {
				case "identical_copy":
					r.scenePolicy = cloneNativeScenePolicy(r.scenePolicy)
				case "changed_copy":
					r.scenePolicy = cloneNativeScenePolicy(r.scenePolicy)
					r.scenePolicy.Policy = []byte("[]")
				case "policy_alias":
					policy.Policy[0] = '['
				case "expectation_alias":
					policy.Scene.Expected.RequiredChecks[0] = "changed"
				}
				_, childErr := nativeRead(ctx, r, who, TransferColorProfile{}, nativeReadKey{"child", "seal"}, clonePhaseProof, func(SQLTransferSourceReader) (phaseProof, error) { return phaseProof{}, nil }, phaseTestWitness)
				return phaseProof{[]int{1}}, childErr
			}, phaseTestWitness)
			if mode == "identical_copy" {
				if err != nil || len(out.values) != 1 {
					t.Fatalf("equal decoded value rejected: %v %v", out, err)
				}
			} else if err == nil || out.values != nil {
				t.Fatalf("changed scene policy accepted: %v %v", out, err)
			}
		})
	}
}

type phaseLatePolicyObjects struct {
	policy *createScenePolicy
	opens  int
}

func (o *phaseLatePolicyObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	o.opens++
	if o.opens == 2 {
		o.policy.Policy[0] = '['
	}
	return io.NopCloser(strings.NewReader("{}")), nil
}
func TestNativeReadPhaseSceneAliasMutationAtFinalFence(t *testing.T) {
	for _, mode := range []string{"physical_open", "last_witness"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			who := phaseTestIdentity()
			policy := &createScenePolicy{Policy: []byte("{}")}
			objects := &phaseLatePolicyObjects{policy: policy}
			obj := objectstore.Object{Key: "original/key", VersionID: "v1", ContentType: "application/json", Bytes: 2, SHA256: "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"}
			got, err := nativeRead(ctx, SQLTransferSourceReader{scenePolicy: policy, Objects: objects}, who, TransferColorProfile{}, nativeReadKey{"root", "seal"}, clonePhaseProof, func(r SQLTransferSourceReader) (phaseProof, error) {
				if mode == "physical_open" {
					if _, err := v2input.ReadPinnedSemanticDocument(ctx, r.Objects, obj, v2input.Ref{ArtifactID: uuid.New(), SHA256: obj.SHA256}); err != nil {
						return phaseProof{}, err
					}
				}
				return phaseProof{[]int{1}}, nil
			}, func(SQLTransferSourceReader, phaseProof) func(context.Context) error {
				return func(context.Context) error {
					if mode == "last_witness" {
						policy.Policy[0] = '['
					}
					return nil
				}
			})
			if err == nil || got.values != nil {
				t.Fatalf("late operator policy mutation escaped: %v %v", got, err)
			}
		})
	}
}
