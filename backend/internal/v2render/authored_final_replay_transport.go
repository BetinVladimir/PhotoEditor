package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// BindReplayTransport is only for an already staged immutable BLOCKED final
// consumer. The protected writer rechecks actual ready replay evidence and
// retains its base snapshot. It never stages an effective profile as new Params.
func (s SQLAuthoredFinalAdmission) BindReplayTransport(ctx context.Context, tenant uuid.UUID, base AuthoredFinalTaskPlan, entries []v2input.Entry) (AuthoredFinalTaskPlan, error) {
	var empty AuthoredFinalTaskPlan
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s.Scheduler == nil || tenant == uuid.Nil || base.Params.Params.TenantID != tenant {
		return empty, v2input.ErrInvalid
	}
	if err := base.validateFrozen(); err != nil {
		return empty, err
	}
	owned := append([]v2input.Entry(nil), entries...)
	sort.Slice(owned, func(i, j int) bool { return bytes.Compare(owned[i].Ref.ArtifactID[:], owned[j].Ref.ArtifactID[:]) < 0 })
	if len(owned) < len(base.Entries) || len(owned) > 4096 {
		return empty, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]v2input.Entry{}
	for _, e := range owned {
		if !validLayerArtifactRef(e.Ref) || e.Contract == "" || len(e.Contract) > 120 {
			return empty, v2input.ErrInvalid
		}
		if _, ok := seen[e.Ref.ArtifactID]; ok {
			return empty, v2input.ErrInvalid
		}
		seen[e.Ref.ArtifactID] = e
	}
	for _, e := range base.Entries {
		if seen[e.Ref.ArtifactID] != e {
			return empty, v2input.ErrInvalid
		}
	}
	var selected AuthoredFinalTaskPlan
	err := database.TenantTransaction(ctx, s.Scheduler, tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		proof, err := BindAuthoredNativeReplayConsumerTransport(ctx, tx, tenant, base.Task.ID, base.Entries, owned)
		if err != nil {
			return err
		}
		selected, err = compileAuthoredFinalBoundTransport(base, owned, proof.CanonicalReadback())
		return err // Any mismatch rolls back the protected writer too.
	})
	if err != nil {
		return empty, err
	}
	return selected, nil
}

// The SQL proof is necessary, but its returned document is also decoded closed
// and the complete deterministic transport delta is recomputed independently.
func compileAuthoredFinalBoundTransport(base AuthoredFinalTaskPlan, entries []v2input.Entry, raw []byte) (AuthoredFinalTaskPlan, error) {
	fail := func() (AuthoredFinalTaskPlan, error) { return AuthoredFinalTaskPlan{}, v2input.ErrInvalid }
	if base.validateFrozen() != nil {
		return fail()
	}
	var binding struct {
		Snapshot    json.RawMessage `json:"base_snapshot"`
		BaseDigest  string          `json:"base_input_digest"`
		Digest      string          `json:"final_input_digest"`
		Entries     string          `json:"entries_jcs"`
		Roots       []v2input.Entry `json:"request_evidence_roots"`
		BaseProfile json.RawMessage `json:"base_profile"`
		Profile     json.RawMessage `json:"final_profile"`
	}
	if decodeAuthoredParams(raw, &binding) != nil || len(binding.Snapshot) == 0 || bytes.Equal(binding.Snapshot, []byte("null")) || binding.BaseDigest != base.Task.InputDigest || len(binding.Roots) < 2 {
		return fail()
	}
	expectedEntries, err := canonical.Marshal(entries)
	if err != nil || binding.Entries != string(expectedEntries) {
		return fail()
	}
	wire, err := canonical.Bytes(binding.BaseProfile)
	if err != nil || !bytes.Equal(wire, base.Task.ExecutionProfile) {
		return fail()
	}
	requests, evidences := 0, 0
	for _, root := range binding.Roots {
		if root.Contract == "AuthoredNativeReplayRequest" {
			requests++
		}
		if root.Contract == "AuthoredNativeReplayEvidence" {
			evidences++
		}
		if root.Contract != "AuthoredNativeReplayRequest" && root.Contract != "AuthoredNativeReplayEvidence" {
			return fail()
		}
		found := false
		for _, e := range entries {
			if e == root {
				found = true
				break
			}
		}
		if !found {
			return fail()
		}
	}
	if requests < 1 || evidences < 1 {
		return fail()
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{base.Task.ID, "render.compose", "2", entries})
	if err != nil || digest != binding.Digest {
		return fail()
	}
	var profile map[string]json.RawMessage
	var resources map[string]json.RawMessage
	if json.Unmarshal(base.Task.ExecutionProfile, &profile) != nil || json.Unmarshal(profile["resource_estimate"], &resources) != nil {
		return fail()
	}
	if _, exists := profile["input_index_scratch_bytes"]; exists {
		return fail()
	}
	var scratch int64
	if json.Unmarshal(resources["scratch_bytes"], &scratch) != nil || scratch < 0 {
		return fail()
	}
	allocation := int64(len(entries)) * 512
	if allocation < 16777216 {
		allocation = 16777216
	}
	if scratch > 9007199254740991-allocation {
		return fail()
	}
	resources["scratch_bytes"], _ = json.Marshal(scratch + allocation)
	profile["resource_estimate"], err = canonical.Marshal(resources)
	if err != nil {
		return fail()
	}
	profile["input_index_scratch_bytes"], _ = json.Marshal(allocation)
	profile["contracts_sha"], _ = json.Marshal("338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b")
	expected, err := canonical.Marshal(profile)
	if err != nil {
		return fail()
	}
	actual, err := canonical.Bytes(binding.Profile)
	if err != nil || !bytes.Equal(expected, actual) {
		return fail()
	}
	selected := base
	selected.Entries = append([]v2input.Entry(nil), entries...)
	selected.Task.ExecutionProfile = append(json.RawMessage(nil), actual...)
	selected.Task.InputDigest = digest
	sum := sha256.Sum256(actual)
	selected.ProfileSHA = hex.EncodeToString(sum[:])
	selected.fingerprint = ""
	selected.fingerprint, err = canonical.Hash(selected)
	if err != nil {
		return fail()
	}
	return selected, nil
}

// AdmitReplayTransport resumes B-DAG after actual replay prerequisites are ready.
// The base shell must already exist: no Stage call may overwrite its snapshot.
func (s SQLAuthoredFinalAdmission) AdmitReplayTransport(ctx context.Context, tenant, parent uuid.UUID, ordinal int, base AuthoredFinalTaskPlan, entries []v2input.Entry, bucket string, objects artifactjson.Objects) error {
	if base.Params.Params.ParentRunID != parent || len(base.Params.Params.PhaseReceipts)+1 != ordinal || bucket == "" || objects == nil {
		return v2input.ErrInvalid
	}
	selected, err := s.BindReplayTransport(ctx, tenant, base, entries)
	if err != nil {
		return err
	}
	graph, err := s.PublishTransport(ctx, tenant, parent, ordinal, selected, bucket, objects)
	if err != nil {
		return err
	}
	return s.Seal(ctx, tenant, parent, ordinal, selected, graph)
}
