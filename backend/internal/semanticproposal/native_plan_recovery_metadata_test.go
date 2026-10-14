package semanticproposal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestNativePlanRecoveryPreservesOriginalCostsAndOwnsReport(t *testing.T) {
	costs := v2input.SemanticQualifiedCosts{RAMBytes: 123, VRAMBytes: 256, ScratchBytes: 321}
	profile, err := v2input.CompileSemanticDetectorProfile(8, 8, 1024, costs, 1<<40, 1<<40, 1<<40, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	task := v2input.DraftTask{Operation: "face.detect", ExecutionProfile: profile}
	report := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	metadata, err := nativePlanRecoveryFromTask(task, &report, costs, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	original := report
	report.ArtifactID = uuid.New()
	if metadata.Report == nil || *metadata.Report != original || metadata.Costs != costs {
		t.Fatal("lost original compiler inputs or aliased report")
	}
	raw, err := canonical.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativePlanRecoveryMetadata(raw, task)
	if err != nil || decoded.Report == nil || *decoded.Report != original || decoded.Costs != costs {
		t.Fatal("original recovery metadata refused", err)
	}
	for _, field := range []string{"schema_version", "ram_limit_bytes", "vram_limit_bytes", "scratch_limit_bytes", "output_limit_bytes", "reserved_bytes", "qualification_report", "unknown"} {
		t.Run(field, func(t *testing.T) {
			var changed map[string]any
			if json.Unmarshal(raw, &changed) != nil {
				t.Fatal("fixture")
			}
			changed[field] = 0
			if field == "unknown" {
				changed[field] = true
			}
			body, err := canonical.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeNativePlanRecoveryMetadata(body, task)
			if err == nil || got.Report != nil || got.Schema != 0 {
				t.Fatal("changed recovery metadata exposed compiler inputs")
			}
		})
	}
}

func TestNativeSkinRecoveryRefusesSyntheticQualification(t *testing.T) {
	task := v2input.DraftTask{Operation: "mask.face_skin", ExecutionProfile: json.RawMessage(`{"backend":"cpu","resource_estimate":{"ram_bytes":1024,"vram_bytes":0,"scratch_bytes":2048,"output_bytes":4096}}`)}
	if _, err := nativePlanRecoveryFromTask(task, nil, v2input.SemanticQualifiedCosts{}, 16<<20); err != nil {
		t.Fatal(err)
	}
	report := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	for _, value := range []struct {
		report *v2input.Ref
		costs  v2input.SemanticQualifiedCosts
	}{{&report, v2input.SemanticQualifiedCosts{}}, {nil, v2input.SemanticQualifiedCosts{RAMBytes: 1}}} {
		if got, err := nativePlanRecoveryFromTask(task, value.report, value.costs, 16<<20); err == nil || got.Schema != 0 {
			t.Fatal("CPU derivation adopted synthetic model qualification")
		}
	}
}

func TestNativeRecoveryRequestKeyStrictOptionalLocator(t *testing.T) {
	profile := json.RawMessage(`{"backend":"cuda","resource_estimate":{"ram_bytes":1024,"vram_bytes":0,"scratch_bytes":2048,"output_bytes":4096}}`)
	task := v2input.DraftTask{Operation: "face.detect", ExecutionProfile: profile}
	report := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	metadata, err := nativePlanRecoveryFromTask(task, &report, v2input.SemanticQualifiedCosts{}, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "original-request"} {
		metadata.RequestKey = key
		raw, err := canonical.Marshal(metadata)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeNativePlanRecoveryMetadata(raw, task)
		if err != nil || got.RequestKey != key {
			t.Fatal("original key format refused", err)
		}
	}
	metadata.RequestKey = "original-request"
	raw, err := canonical.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	task.Operation = "unsupported.operation"
	if got, err := decodeNativePlanRecoveryMetadata(raw, task); err == nil || got.Schema != 0 {
		t.Fatal("unsupported operation adopted original key")
	}
	task.Operation = "face.detect"
	for _, bad := range []any{"", nil, 42, strings.Repeat("x", 257)} {
		var changed map[string]any
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		changed["request_key"] = bad
		body, err := canonical.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := decodeNativePlanRecoveryMetadata(body, task); err == nil || got.Schema != 0 {
			t.Fatal("invalid explicit key accepted")
		}
	}
}

func TestPersonRecoveryRequiresOriginalQualifiedCUDAProfile(t *testing.T) {
	costs := v2input.SemanticQualifiedCosts{RAMBytes: 123, VRAMBytes: 1 << 30, ScratchBytes: 321}
	profile, err := v2input.CompileSemanticPersonProfile(8, 8, 1024, costs, 1<<40, 12<<30, 1<<40, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	task := v2input.DraftTask{Operation: "mask.person", ExecutionProfile: profile}
	report := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	metadata, err := nativePlanRecoveryFromTask(task, &report, costs, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	metadata.RequestKey = "original-person-parent"
	raw, err := canonical.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeNativePlanRecoveryMetadata(raw, task)
	if err != nil || got.RequestKey != metadata.RequestKey || got.Costs != costs || got.Report == nil || *got.Report != report {
		t.Fatal("person original compiler inputs lost", err)
	}
	for _, backend := range []string{"cpu", "mps", "unknown"} {
		changed := task
		changed.ExecutionProfile = json.RawMessage(strings.Replace(string(profile), `"cuda"`, `"`+backend+`"`, 1))
		if value, err := decodeNativePlanRecoveryMetadata(raw, changed); err == nil || value.Schema != 0 {
			t.Fatal("non-CUDA person profile accepted", backend)
		}
	}
	for _, cost := range []v2input.SemanticQualifiedCosts{{}, {RAMBytes: metadata.RAM + 1, VRAMBytes: 1}, {VRAMBytes: metadata.VRAM + 1}, {VRAMBytes: 1, ScratchBytes: metadata.Scratch + 1}} {
		if value, err := nativePlanRecoveryFromTask(task, &report, cost, metadata.Reserved); err == nil || value.Schema != 0 {
			t.Fatal("invalid person costs accepted", cost)
		}
	}
	if value, err := nativePlanRecoveryFromTask(task, nil, costs, metadata.Reserved); err == nil || value.Schema != 0 {
		t.Fatal("missing person qualification accepted")
	}
}

func TestOrdinaryContinuationIsPersonOnlyAndStrict(t *testing.T) {
	task := v2input.DraftTask{Operation: "mask.person", ExecutionProfile: json.RawMessage(`{"backend":"cuda","resource_estimate":{"ram_bytes":1024,"vram_bytes":256,"scratch_bytes":2048,"output_bytes":4096}}`)}
	report := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	m, err := nativePlanRecoveryFromTask(task, &report, v2input.SemanticQualifiedCosts{VRAMBytes: 128}, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"", "ordinary_parent", "manual_parent", "unknown"} {
		m.Continuation = mode
		raw, err := canonical.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeNativePlanRecoveryMetadata(raw, task)
		if mode == "" {
			if err != nil || got.Continuation != mode {
				t.Fatal("original continuation", err)
			}
		} else if err == nil {
			t.Fatal("unknown continuation accepted")
		}
	}
	m.Continuation = "ordinary_parent"
	raw, _ := canonical.Marshal(m)
	task.Operation = "face.detect"
	if _, err := decodeNativePlanRecoveryMetadata(raw, task); err == nil {
		t.Fatal("person continuation accepted on detector")
	}
}

func TestPersonRecoveryProfileVersionIsOwnedByExactIdentityShape(t *testing.T) {
	base := map[string]any{"backend": "cuda", "resource_estimate": map[string]any{"ram_bytes": 1024, "vram_bytes": 256, "scratch_bytes": 16779264, "output_bytes": 4096}}
	report := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	for _, version := range []int{0, 1} {
		profile := map[string]any{}
		for k, v := range base {
			profile[k] = v
		}
		if version == 1 {
			profile["model_lock_id"] = strings.Repeat("a", 64)
			profile["model_lock_sha"] = strings.Repeat("a", 64)
			profile["runtime_lock_sha"] = strings.Repeat("b", 64)
			profile["device_profile_sha"] = strings.Repeat("c", 64)
		}
		raw, err := canonical.Marshal(profile)
		if err != nil {
			t.Fatal(err)
		}
		task := v2input.DraftTask{Operation: "mask.person", ExecutionProfile: raw}
		metadata, err := nativePlanRecoveryFromTask(task, &report, v2input.SemanticQualifiedCosts{VRAMBytes: 128}, 16<<20)
		if err != nil || metadata.PersonProfileVersion != version {
			t.Fatal("profile version", version, err)
		}
		saved, err := canonical.Marshal(metadata)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeNativePlanRecoveryMetadata(saved, task)
		if err != nil || decoded.PersonProfileVersion != version {
			t.Fatal("exact version restore", err)
		}
		metadata.PersonProfileVersion = 2
		changed, _ := canonical.Marshal(metadata)
		if _, err := decodeNativePlanRecoveryMetadata(changed, task); err == nil {
			t.Fatal("unknown profile version accepted")
		}
		if version == 1 {
			for _, key := range []string{"model_lock_id", "model_lock_sha", "runtime_lock_sha", "device_profile_sha"} {
				previous := profile[key]
				delete(profile, key)
				bad, _ := canonical.Marshal(profile)
				task.ExecutionProfile = bad
				if _, err := nativePlanRecoveryFromTask(task, &report, v2input.SemanticQualifiedCosts{VRAMBytes: 128}, 16<<20); err == nil {
					t.Fatal("partial identities accepted", key)
				}
				profile[key] = previous
			}
			profile["model_lock_id"] = strings.Repeat("d", 64)
			bad, _ := canonical.Marshal(profile)
			task.ExecutionProfile = bad
			if _, err := nativePlanRecoveryFromTask(task, &report, v2input.SemanticQualifiedCosts{VRAMBytes: 128}, 16<<20); err == nil {
				t.Fatal("contradictory model identity accepted")
			}
		}
	}
}

func TestFaceRecoveryProfileVersionIsOwnedByExactIdentityShape(t *testing.T) {
	base := map[string]any{"backend": "cuda", "resource_estimate": map[string]any{"ram_bytes": 1024, "vram_bytes": 256, "scratch_bytes": 16779264, "output_bytes": 4096}}
	report := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	for _, operation := range []string{"face.detect", "face.landmarks", "face.parse"} {
		for _, version := range []int{0, 1} {
			profile := map[string]any{}
			for k, v := range base {
				profile[k] = v
			}
			if version == 1 {
				profile["model_lock_id"] = strings.Repeat("a", 64)
				profile["model_lock_sha"] = strings.Repeat("a", 64)
				profile["runtime_lock_sha"] = strings.Repeat("b", 64)
				profile["device_profile_sha"] = strings.Repeat("c", 64)
			}
			raw, err := canonical.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}
			task := v2input.DraftTask{Operation: operation, ExecutionProfile: raw}
			metadata, err := nativePlanRecoveryFromTask(task, &report, v2input.SemanticQualifiedCosts{VRAMBytes: 128}, 16<<20)
			if err != nil || metadata.FaceProfileVersion != version {
				t.Fatal("profile version", version, err)
			}
			metadata.RequestKey = "original-target-parent"
			saved, err := canonical.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeNativePlanRecoveryMetadata(saved, task)
			if err != nil || decoded.FaceProfileVersion != version {
				t.Fatal("exact version restore", err)
			}
			metadata.FaceProfileVersion = 2
			changed, _ := canonical.Marshal(metadata)
			if _, err := decodeNativePlanRecoveryMetadata(changed, task); err == nil {
				t.Fatal("unknown profile version accepted")
			}
			if version == 1 {
				for _, key := range []string{"model_lock_id", "model_lock_sha", "runtime_lock_sha", "device_profile_sha"} {
					previous := profile[key]
					delete(profile, key)
					bad, _ := canonical.Marshal(profile)
					task.ExecutionProfile = bad
					if _, err := nativePlanRecoveryFromTask(task, &report, v2input.SemanticQualifiedCosts{VRAMBytes: 128}, 16<<20); err == nil {
						t.Fatal("partial identities accepted", key)
					}
					profile[key] = previous
				}
				profile["model_lock_id"] = strings.Repeat("d", 64)
				bad, _ := canonical.Marshal(profile)
				task.ExecutionProfile = bad
				if _, err := nativePlanRecoveryFromTask(task, &report, v2input.SemanticQualifiedCosts{VRAMBytes: 128}, 16<<20); err == nil {
					t.Fatal("contradictory model identity accepted")
				}
			}
		}

	}
}

func TestSkinRecoveryProfileVersionRetainsExactIndexBudget(t *testing.T) {
	for _, version := range []int{0, 1} {
		profile := map[string]any{"backend": "cpu", "resource_estimate": map[string]any{"ram_bytes": 1024, "vram_bytes": 0, "scratch_bytes": (16 << 20) + 2048, "output_bytes": 4096}}
		if version == 1 {
			profile["input_index_scratch_bytes"] = 16 << 20
		}
		raw, _ := canonical.Marshal(profile)
		task := v2input.DraftTask{Operation: "mask.face_skin", ExecutionProfile: raw}
		metadata, err := nativePlanRecoveryFromTask(task, nil, v2input.SemanticQualifiedCosts{}, 16<<20)
		if err != nil || metadata.SkinProfileVersion != version || metadata.PersonProfileVersion != 0 || metadata.FaceProfileVersion != 0 {
			t.Fatal("skin version", version, err)
		}
		saved, _ := canonical.Marshal(metadata)
		restored, err := decodeNativePlanRecoveryMetadata(saved, task)
		if err != nil || restored.SkinProfileVersion != version {
			t.Fatal("exact skin recovery", err)
		}
		for _, field := range []string{"skin_profile_version", "face_profile_version", "person_profile_version"} {
			var changed map[string]any
			json.Unmarshal(saved, &changed)
			changed[field] = 2
			bad, _ := canonical.Marshal(changed)
			if _, err := decodeNativePlanRecoveryMetadata(bad, task); err == nil {
				t.Fatal("foreign/unknown profile version accepted", field)
			}
		}
		for _, marker := range []any{nil, 0, (16 << 20) + 1, 1.5, "16777216"} {
			profile["input_index_scratch_bytes"] = marker
			bad, _ := canonical.Marshal(profile)
			task.ExecutionProfile = bad
			if _, err := nativePlanRecoveryFromTask(task, nil, v2input.SemanticQualifiedCosts{}, 16<<20); err == nil {
				t.Fatal("invalid marker accepted", marker)
			}
		}
	}
}
