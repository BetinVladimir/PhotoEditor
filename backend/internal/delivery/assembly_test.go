package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func assemblyFixture(t *testing.T, count int) (FrozenInput, []byte, []Slot) {
	t.Helper()
	ref, raw, policy, configs := freezeFixture(t)
	var original v2input.SessionInputSnapshot
	if json.Unmarshal(raw, &original) != nil {
		t.Fatal("fixture")
	}
	items := make([]v2input.SessionSnapshotItem, count)
	for i := range items {
		items[i] = original.Items[0]
		items[i].Ordinal = i
		items[i].Binding.AssetVersionID = uuid.New()
	}
	_, raw, err := v2input.CompilePinnedSessionInputSnapshot(original.SnapshotID, items)
	if err != nil {
		t.Fatal(err)
	}
	ref.SHA256 = digest(raw)
	frozen, _, _, err := CompileFrozenInput(ref, raw, policy, configs)
	if err != nil {
		t.Fatal(err)
	}
	slots := make([]Slot, count)
	for i := range slots {
		id := uuid.New()
		export := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("encoded"))}
		slots[i] = Slot{Ordinal: i, Binding: items[i].Binding, PresetID: configs[0].ID, PresetSHA: configs[0].SHA, Required: true, State: "ready", ExportID: &id, Export: &export}
	}
	return frozen, raw, slots
}
func TestAssemblyShards501AndOwnedInputs(t *testing.T) {
	frozen, snapshot, slots := assemblyFixture(t, 501)
	var shards []SlotShard
	var purposes []string
	result, raw, hash, err := BuildAssemblyInput(context.Background(), frozen, snapshot, slots, func(_ context.Context, purpose string, body []byte) (v2input.Ref, error) {
		var shard SlotShard
		if json.Unmarshal(body, &shard) != nil {
			t.Fatal("invalid shard")
		}
		shards = append(shards, shard)
		purposes = append(purposes, purpose)
		// Publication callbacks cannot alter later shard inputs through the caller's pointers.
		slots[500].Export.SHA256 = digest([]byte("changed"))
		frozen.Policy.RequiredPresets[0] = uuid.New()
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(body)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(shards) != 2 || len(shards[0].Items) != 500 || len(shards[1].Items) != 1 || result.SlotCount != 501 || digest(raw) != hash {
		t.Fatal("bad sharding")
	}
	if shards[1].Items[0].Export.SHA256 == slots[500].Export.SHA256 {
		t.Fatal("borrowed input")
	}
	if purposes[0] != "v30_delivery_slots_000" || purposes[1] != "v30_delivery_slots_001" {
		t.Fatal("unstable purpose")
	}
}
func TestAssemblyPreflightRefusesEntireMatrixBeforePublication(t *testing.T) {
	for _, kind := range []string{"pending", "missing", "duplicate", "duplicate_export_id", "binding", "omitted", "preset"} {
		t.Run(kind, func(t *testing.T) {
			frozen, snapshot, slots := assemblyFixture(t, 2)
			calls := 0
			switch kind {
			case "pending":
				slots[1].State = "pending"
			case "missing":
				slots = slots[:1]
			case "duplicate":
				slots[1].Ordinal = 0
			case "duplicate_export_id":
				id := *slots[0].ExportID
				slots[1].ExportID = &id
			case "binding":
				slots[1].Binding.AssetVersionID = uuid.New()
			case "omitted":
				slots[1].State = "omitted"
			case "preset":
				slots[1].PresetSHA = digest([]byte("foreign"))
			}
			_, _, _, err := BuildAssemblyInput(context.Background(), frozen, snapshot, slots, func(context.Context, string, []byte) (v2input.Ref, error) { calls++; return v2input.Ref{}, nil })
			if err == nil || calls != 0 {
				t.Fatal("published invalid preflight", err, calls)
			}
		})
	}
}
func TestAssemblyRefusesPublisherSubstitutionAndCancellation(t *testing.T) {
	frozen, snapshot, slots := assemblyFixture(t, 1)
	_, _, _, err := BuildAssemblyInput(context.Background(), frozen, snapshot, slots, func(context.Context, string, []byte) (v2input.Ref, error) {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("wrong"))}, nil
	})
	if err == nil {
		t.Fatal("changed publication accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, _, _, err = BuildAssemblyInput(ctx, frozen, snapshot, slots, func(context.Context, string, []byte) (v2input.Ref, error) { calls++; return v2input.Ref{}, nil })
	if err != context.Canceled || calls != 0 {
		t.Fatal(err, calls)
	}
}


func TestAssemblyPreservesMetadataPolicyAcrossShards(t *testing.T) {
    for _, policy := range []string{"strip", "copyright", "approved_exif"} {
        t.Run(policy, func(t *testing.T) {
            frozen, snapshot, slots := assemblyFixture(t, 501)
            frozen.Policy.MetadataPolicy = policy
            count := 0
            result, raw, hash, err := BuildAssemblyInput(context.Background(), frozen, snapshot, slots,
                func(_ context.Context, _ string, body []byte) (v2input.Ref, error) {
                    var shard SlotShard
                    if json.Unmarshal(body, &shard) != nil { t.Fatal("invalid shard") }
                    for _, slot := range shard.Items {
                        if slot.State != "ready" || slot.Export == nil { t.Fatal("lost export") }
                    }
                    count += len(shard.Items)
                    return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(body)}, nil
                })
            if err != nil || count != 501 || result.Policy.MetadataPolicy != policy || digest(raw) != hash {
                t.Fatal("metadata assembly mismatch", err, count)
            }
            var decoded AssemblyInput
            if json.Unmarshal(raw, &decoded) != nil || decoded.Policy.MetadataPolicy != policy {
                t.Fatal("metadata policy lost in canonical assembly")
            }
        })
    }
}

func TestAssemblyUnknownMetadataRefusesBeforePublication(t *testing.T) {
    frozen, snapshot, slots := assemblyFixture(t, 1)
    frozen.Policy.MetadataPolicy = "unknown"
    calls := 0
    _, _, _, err := BuildAssemblyInput(context.Background(), frozen, snapshot, slots,
        func(context.Context, string, []byte) (v2input.Ref, error) { calls++; return v2input.Ref{}, nil })
    if err == nil || calls != 0 { t.Fatal("unknown metadata policy published", err, calls) }
}

func TestAssemblyCancellationAfterPublicationStopsBeforeNextShard(t *testing.T) {
    for _, kind := range []string{"valid_ref", "invalid_ref", "publisher_error"} {
        t.Run(kind, func(t *testing.T) {
            frozen, snapshot, slots := assemblyFixture(t, 501)
            ctx, cancel := context.WithCancel(context.Background())
            defer cancel()
            publicationError := errors.New("publication failed")
            calls := 0
            result, raw, hash, err := BuildAssemblyInput(ctx, frozen, snapshot, slots,
                func(_ context.Context, _ string, body []byte) (v2input.Ref, error) {
                    calls++
                    cancel()
                    if kind == "publisher_error" { return v2input.Ref{}, publicationError }
                    // Invalid returned refs must not mask a cancelled attempt.
                    if kind == "invalid_ref" { return v2input.Ref{}, nil }
                    return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(body)}, nil
                })
            expected := error(context.Canceled)
            if kind == "publisher_error" { expected = publicationError }
            if err != expected || calls != 1 || raw != nil || hash != "" || result.SchemaVersion != 0 {
                t.Fatal("cancelled publication produced assembly or lost primary error", err, calls)
            }
        })
    }
}

func TestAssemblyRejectsInvalidFrozenMetadataApprovalBeforePublication(t *testing.T) {
    cases := []struct { name, policy string; fields []string }{
        {"strip_gps", "strip", []string{"gps"}},
        {"copyright_gps", "copyright", []string{"gps"}},
        {"unknown_field", "approved_exif", []string{"private_embedding"}},
        {"duplicate", "approved_exif", []string{"gps", "gps"}},
        {"noncanonical_order", "approved_exif", []string{"gps", "author"}},
    }
    for _, test := range cases {
        t.Run(test.name, func(t *testing.T) {
            frozen, snapshot, slots := assemblyFixture(t, 1)
            frozen.Policy.MetadataPolicy = test.policy
            frozen.Policy.ApprovedFields = test.fields
            calls := 0
            _, _, _, err := BuildAssemblyInput(context.Background(), frozen, snapshot, slots,
                func(context.Context, string, []byte) (v2input.Ref, error) { calls++; return v2input.Ref{}, nil })
            if err == nil || calls != 0 { t.Fatal("invalid approval reached publication", err, calls) }
        })
    }
}

func TestAssemblyOwnsApprovedMetadataConsentAcrossPublication(t *testing.T) {
    frozen, snapshot, slots := assemblyFixture(t, 501)
    frozen.Policy.MetadataPolicy = "approved_exif"
    frozen.Policy.ApprovedFields = []string{"author", "gps"}
    result, raw, _, err := BuildAssemblyInput(context.Background(), frozen, snapshot, slots,
        func(_ context.Context, _ string, body []byte) (v2input.Ref, error) {
            frozen.Policy.ApprovedFields[1] = "private_embedding"
            return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(body)}, nil
        })
    if err != nil { t.Fatal(err) }
    var decoded AssemblyInput
    if json.Unmarshal(raw, &decoded) != nil || len(result.Policy.ApprovedFields) != 2 ||
        result.Policy.ApprovedFields[0] != "author" || result.Policy.ApprovedFields[1] != "gps" ||
        len(decoded.Policy.ApprovedFields) != 2 || decoded.Policy.ApprovedFields[1] != "gps" {
        t.Fatal("approval was changed through caller-owned input")
    }
}
