package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"testing"
)

func legacyEstimate(t *testing.T, op string, pixels int64, s applyLegacySizing) ResourceEstimate {
	t.Helper()
	base, err := applyProfile(pixels, 777)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := applyLegacyResourceProfile(op, base, s)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Estimate ResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(raw, &p) != nil {
		t.Fatal("profile JSON")
	}
	return p.Estimate
}

func TestLegacyApplyResourcePeaksFollowWorkerLifetimes(t *testing.T) {
	const p int64 = 6032 * 4028
	const header int64 = 8192
	const doc int64 = 1 << 20
	rgb := 12*p + header
	mask := 4*p + header
	cases := []struct {
		name, op        string
		s               applyLegacySizing
		scratch, output int64
	}{
		{"fixed-resolve", "style.resolve", applyLegacySizing{}, doc, doc},
		{"adaptive-resolve-alpha", "style.resolve", applyLegacySizing{Adaptive: true, Alpha: true}, rgb + 2*p + header, 65*65*65*12 + header + doc},
		{"fixed-color", "image.color", applyLegacySizing{}, 2 * rgb, rgb + 2*doc},
		{"lut33-color", "image.color", applyLegacySizing{LUTSize: 33}, 2*rgb + 33*33*33*12 + header, rgb + 2*doc},
		{"lut65-color", "image.color", applyLegacySizing{LUTSize: 65}, 2*rgb + 65*65*65*12 + header, rgb + 2*doc},
		{"local-no-mask", "image.local", applyLegacySizing{}, 3 * rgb, rgb + 2*doc},
		{"local-person-and-three-regions", "image.local", applyLegacySizing{Masks: 4}, 3*rgb + 4*mask, rgb + 2*doc},
		{"quality-target-only", "quality.check", applyLegacySizing{}, rgb, doc},
		{"quality-baseline-alpha-protection", "quality.check", applyLegacySizing{Baseline: true, Alpha: true, Masks: 2}, 2*rgb + 3*mask, doc},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := legacyEstimate(t, c.op, p, c.s)
			if e.ScratchBytes != c.scratch || e.OutputBytes != c.output || e.RAMBytes != 3<<30 || e.PixelCount != p {
				t.Fatalf("incorrect phase peak: %+v", e)
			}
		})
	}
}

func TestLegacyApplyScratchActuallyAdmitsOverlappingReservations(t *testing.T) {
	const pixels int64 = 6032 * 4028
	rgb := 12*pixels + 8192
	old, err := applyProfile(pixels, 777)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Estimate ResourceEstimate `json:"resource_estimate"`
	}
	json.Unmarshal(old, &p)
	for _, c := range []struct {
		op           string
		reservations []int64
		s            applyLegacySizing
	}{
		{"image.color", []int64{rgb, rgb}, applyLegacySizing{}},
		{"image.local", []int64{rgb, rgb, rgb}, applyLegacySizing{}},
		{"quality.check", []int64{rgb, rgb}, applyLegacySizing{Baseline: true}},
	} {
		var peak int64
		for _, n := range c.reservations {
			peak += n
		}
		if peak <= p.Estimate.ScratchBytes {
			t.Fatal("regression did not expose old undersizing")
		}
		fixed := legacyEstimate(t, c.op, pixels, c.s)
		if fixed.ScratchBytes < peak {
			t.Fatalf("%s still cannot materialize concurrent arrays", c.op)
		}
	}
}

func TestLegacyApplyResourceBoundsAndUnrelatedProfilePreserved(t *testing.T) {
	base, _ := applyProfile(1, 0)
	for _, s := range []applyLegacySizing{{Masks: -1}, {Masks: 161}, {LUTSize: 32}, {LUTSize: 66}} {
		if _, e := applyLegacyResourceProfile("image.local", base, s); e == nil {
			t.Fatal("invalid resource shape accepted")
		}
	}
	for _, pixels := range []int64{0, -1, 200000001} {
		raw, _ := applyProfile(pixels, 0)
		if _, e := applyLegacyResourceProfile("image.color", raw, applyLegacySizing{}); e == nil {
			t.Fatal("pixel bounds accepted")
		}
	}
	raw, e := applyLegacyResourceProfile("unrelated.operation", base, applyLegacySizing{})
	if e != nil || string(raw) != string(base) {
		t.Fatal("unrelated profile changed")
	}
	for _, op := range []string{"style.resolve", "image.color", "image.local", "quality.check"} {
		if _, e := applyLegacyResourceProfile(op, []byte(`{}`), applyLegacySizing{}); e == nil {
			t.Fatal("malformed profile accepted")
		}
	}
	// Resolve releases adaptive input arrays before its optional LUT reservation.
	peak := legacyEstimate(t, "style.resolve", 1, applyLegacySizing{Adaptive: true})
	if peak.ScratchBytes != 65*65*65*12+8192 {
		t.Fatal("LUT/input lifetimes incorrectly summed")
	}
}

// This exercises the actual coordinator, registry, params publication and
// PreparedTask materialization. Only storage/publication ports are in memory;
// no worker execution or database acceptance is claimed.
type legacyPhaseFixture struct {
	ApplyPhaseStore // unused paths deliberately panic rather than silently succeed
	item            applyInput
	outputs         map[string]applyOutput
	tasks           []PreparedTask
	plans           []applyNodePlan
	published       [][]byte
	next            string
}

func (s *legacyPhaseFixture) Inputs(context.Context, ApplyLease) ([]applyInput, error) {
	return []applyInput{s.item}, nil
}
func (s *legacyPhaseFixture) Renew(context.Context, ApplyLease) (bool, error) { return true, nil }
func (s *legacyPhaseFixture) Outputs(_ context.Context, _ ApplyLease, op, role string) ([]applyOutput, error) {
	v, ok := s.outputs[op+"/"+role]
	if !ok {
		return nil, nil
	}
	return []applyOutput{v}, nil
}
func (s *legacyPhaseFixture) Register(_ context.Context, _ ApplyLease, p applyNodePlan) (bool, error) {
	s.plans = append(s.plans, p)
	return true, nil
}
func (s *legacyPhaseFixture) Materialize(_ context.Context, _ ApplyLease, p PreparedTask) (bool, error) {
	s.tasks = append(s.tasks, p)
	return true, nil
}
func (s *legacyPhaseFixture) Advance(_ context.Context, _ ApplyLease, to string) (bool, error) {
	s.next = to
	return true, nil
}
func (s *legacyPhaseFixture) Publish(_ context.Context, _ artifactjson.Scope, raw []byte) (artifactjson.Ref, error) {
	s.published = append(s.published, append([]byte(nil), raw...))
	h := sha256.Sum256(raw)
	return artifactjson.Ref{ID: uuid.New(), SHA256: hex.EncodeToString(h[:])}, nil
}

func TestLegacyApplyCoordinatorMaterializesPhaseResourceProfiles(t *testing.T) {
	const pixels int64 = 6032 * 4028
	const rgb int64 = 12*pixels + 8192
	for _, tc := range []struct {
		phase, op, next  string
		lut              int
		alpha            bool
		scratch, output  int64
		adaptive, person bool
	}{
		{"resolve", "style.resolve", "color", 0, false, 1 << 20, 1 << 20, false, false},
		{"color", "image.color", "local", 33, false, 2*rgb + 33*33*33*12 + 8192, rgb + 2*(1<<20), false, false},
		{"color", "image.color", "local", 65, false, 2*rgb + 65*65*65*12 + 8192, rgb + 2*(1<<20), false, false},
		{"local", "image.local", "quality", 0, false, 3 * rgb, rgb + 2*(1<<20), false, false},
		{"quality", "quality.check", "adopt", 0, true, 2*rgb + 4*pixels + 8192, 1 << 20, false, false},
		{"resolve", "style.resolve", "color", 0, true, rgb + 2*pixels + 8192, 65*65*65*12 + 8192 + (1 << 20), true, false},
		{"local", "image.local", "quality", 0, false, 3*rgb + 3*(4*pixels+8192), rgb + 2*(1<<20), false, true},
	} {
		t.Run(fmt.Sprintf("%s-lut%d-adaptive%t-person%t", tc.phase, tc.lut, tc.adaptive, tc.person), func(t *testing.T) {
			chainHash, _ := canonical.Hash(struct {
				Steps []any `json:"steps"`
			}{[]any{}})
			chain, _ := canonical.Marshal(struct {
				Steps []any  `json:"steps"`
				Hash  string `json:"hash"`
			}{[]any{}, chainHash})
			digest := hex.EncodeToString(make([]byte, 32))
			image := review.WorkingImage{Pixels: review.ArtifactRef{ID: uuid.New(), SHA256: digest}, Canvas: review.Canvas{Width: 6032, Height: 4028, Orientation: 1, SourceSHA256: digest, SpaceID: "oriented:" + digest}, Color: review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", ICCSHA256: digest, Encoding: "gamma24"}, DType: "float32", TransformChain: chain}
			if tc.alpha {
				a := review.ArtifactRef{ID: uuid.New(), SHA256: digest}
				image.Alpha = &a
			}
			objects := downloadObjects{body: map[string][]byte{}}
			document := func(v any) styleArtifact {
				raw, e := canonical.Marshal(v)
				if e != nil {
					t.Fatal(e)
				}
				h := sha256.Sum256(raw)
				key := uuid.NewString()
				objects.body[key] = raw
				return styleArtifact{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(h[:]), Bucket: "photos", Key: key, VersionID: "v1", Bytes: int64(len(raw)), MediaType: "application/json"}
			}
			working := document(image)
			resolved := map[string]any{"local": map[string]any{"enabled": false}}
			if tc.lut != 0 {
				resolved["absolute_lut"] = map[string]any{"size": tc.lut}
			}
			if tc.person {
				resolved["local"] = map[string]any{"enabled": true}
				resolved["modules"] = map[string]any{"extensions": map[string]any{"region_adjustments": []any{map[string]any{"region_id": "subject", "selector": "person"}, map[string]any{"region_id": "background", "selector": "background"}}}}
			}
			resolvedDoc := document(resolved)
			fixture := &legacyPhaseFixture{item: applyInput{Ordinal: 0, ItemID: uuid.New(), AssetID: uuid.New(), VersionID: uuid.New(), Image: working, Config: json.RawMessage(`{"style_config":{"mode":"fixed"},"identity_style":true}`)}, outputs: map[string]applyOutput{}}
			out := func(a styleArtifact) applyOutput {
				return applyOutput{Ordinal: 0, ItemID: fixture.item.ItemID, TaskID: uuid.New(), Artifact: a}
			}
			if tc.adaptive {
				fixture.item.Config = json.RawMessage(`{"style_config":{"mode":"adaptive"},"identity_style":true}`)
			}
			if tc.person {
				fixture.item.Config, _ = canonical.Marshal(map[string]any{"style_config": map[string]any{"mode": "fixed"}, "identity_style": true, "person_mask_required": true, "person_backend": "cpu", "person_runtime_lock_id": digest})
				mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: digest}, Canvas: image.Canvas, TransformChain: chain, Selector: "person", Confidence: 1, ModelLockID: &digest}
				fixture.outputs["mask.person/person_mask_ref"] = out(document(mask))
			}
			fixture.outputs["color.stats/stats"] = out(document(map[string]any{"stats": true}))
			fixture.outputs["style.resolve/resolved_style"] = out(resolvedDoc)
			fixture.outputs["image.color/working_image"] = out(working)
			fixture.outputs["image.local/working_image"] = out(working)
			c := ApplyCoordinator{Store: fixture, Publisher: fixture, Objects: objects, Bucket: "photos"}
			ok, e := c.build(context.Background(), ApplyLease{TenantID: uuid.New(), RunID: uuid.New(), Token: uuid.New(), Phase: tc.phase, ItemCount: 1, GraphVersion: 1})
			if e != nil || !ok {
				t.Fatalf("coordinator: %v", e)
			}
			if len(fixture.tasks) != 1 || len(fixture.plans) != 1 || len(fixture.published) != 1 || fixture.next != tc.next {
				t.Fatalf("phase not materialized/advanced: %+v", fixture)
			}
			task := fixture.tasks[0]
			var profile struct {
				Estimate ResourceEstimate `json:"resource_estimate"`
			}
			if e = json.Unmarshal(task.ExecutionProfile, &profile); e != nil {
				t.Fatal(e)
			}
			if task.Operation != tc.op || task.ResourceClass != "cpu" || profile.Estimate.ScratchBytes != tc.scratch || profile.Estimate.OutputBytes != tc.output || profile.Estimate.PixelCount != pixels {
				t.Fatalf("wrong actual materialized profile: %+v %s", task, task.ExecutionProfile)
			}
			h := sha256.Sum256(fixture.published[0])
			if task.Params.SHA256 != hex.EncodeToString(h[:]) {
				t.Fatal("params publication lost")
			}
		})
	}
}
