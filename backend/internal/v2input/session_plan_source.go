package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
)

type frozenCameraOffset struct {
	CameraHash string `json:"camera_hash"`
	Seconds    int    `json:"seconds"`
}

type frozenSessionPolicy struct {
	SchemaVersion      int                  `json:"schema_version"`
	TimeGapSeconds     int                  `json:"time_gap_seconds"`
	NeighborCount      int                  `json:"neighbor_count"`
	ProtectManualPicks bool                 `json:"protect_manual_picks"`
	CameraOffsets      []frozenCameraOffset `json:"camera_offsets"`
	Calibration        Ref                  `json:"calibration"`
}

func parseFrozenPolicy(raw []byte, expectedSHA string) (frozenSessionPolicy, error) {
	var policy frozenSessionPolicy
	if len(raw) == 0 || len(raw) > 1_048_576 || !digestPattern.MatchString(expectedSHA) {
		return policy, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&policy) != nil || policy.SchemaVersion != 1 ||
		policy.TimeGapSeconds < 30 || policy.TimeGapSeconds > 7200 ||
		policy.NeighborCount < 1 || policy.NeighborCount > 64 ||
		!policy.ProtectManualPicks || policy.CameraOffsets == nil ||
		len(policy.CameraOffsets) > 32 || !validRef(policy.Calibration) {
		return frozenSessionPolicy{}, ErrInvalid
	}
	seen := make(map[string]bool, len(policy.CameraOffsets))
	for _, offset := range policy.CameraOffsets {
		if !digestPattern.MatchString(offset.CameraHash) || offset.Seconds < -86400 ||
			offset.Seconds > 86400 || seen[offset.CameraHash] {
			return frozenSessionPolicy{}, ErrInvalid
		}
		seen[offset.CameraHash] = true
	}
	sha, err := canonical.Hash(json.RawMessage(raw))
	if err != nil || sha != expectedSHA {
		return frozenSessionPolicy{}, ErrInvalid
	}
	return policy, nil
}

// SQLSessionPlanSource reads only admission-frozen run configuration through a
// narrow SECURITY DEFINER function. Closure is a repeatable in-memory cursor;
// it performs no storage IO when reopened inside the commit transaction.
type SQLSessionPlanSource struct{ DB *pgxpool.Pool }

func (s SQLSessionPlanSource) Load(ctx context.Context, lease SessionPlanLease) (FrozenSessionPlan, error) {
	if s.DB == nil || !validSessionPlanLease(lease) {
		return FrozenSessionPlan{}, ErrInvalid
	}
	var frozen FrozenSessionPlan
	var raw []byte
	var contextID uuid.UUID
	var contextSHA, policySHA string
	err := s.DB.QueryRow(ctx, `SELECT item_count,policy,policy_sha,
	 context_artifact_id,context_sha,snapshot_digest
	 FROM photo.v22_session_plan_frozen($1,$2)`, lease.TenantID, lease.RunID).
		Scan(&frozen.ItemCount, &raw, &policySHA, &contextID, &contextSHA,
			&frozen.SnapshotDigest)
	if err != nil {
		return FrozenSessionPlan{}, err
	}
	policy, err := parseFrozenPolicy(raw, policySHA)
	if err != nil || frozen.ItemCount < 1 || frozen.ItemCount > 5000 ||
		!digestPattern.MatchString(frozen.SnapshotDigest) ||
		!validRef(Ref{ArtifactID: contextID, SHA256: contextSHA}) {
		return FrozenSessionPlan{}, ErrInvalid
	}
	frozen.Policy = policy
	frozen.PolicySHA = policySHA
	frozen.Context = Ref{ArtifactID: contextID, SHA256: contextSHA}
	return frozen, nil
}

func (s SQLSessionPlanSource) Closure(ctx context.Context, lease SessionPlanLease,
	results []SessionShardResult, params Ref) (SessionPlanClosure, error) {
	frozen, err := s.Load(ctx, lease)
	if err != nil {
		return SessionPlanClosure{}, err
	}
	return CompileSessionPlanClosure(lease.WorkflowID, frozen, results, params)
}

type sessionEntryCursor struct {
	entries []Entry
	index   int
}

func (c *sessionEntryCursor) Next(context.Context) (Entry, bool, error) {
	if c.index >= len(c.entries) {
		return Entry{}, false, nil
	}
	entry := c.entries[c.index]
	c.index++
	return entry, true, nil
}

// CompileSessionPlanClosure includes precisely the refs read by the V22 plan
// adapter. DescriptorShard bodies are not opened: plan never loads their
// nested FaceObservations or source images.
func CompileSessionPlanClosure(workflow uuid.UUID, frozen FrozenSessionPlan,
	results []SessionShardResult, params Ref) (SessionPlanClosure, error) {
	policy, ok := frozen.Policy.(frozenSessionPolicy)
	if workflow == uuid.Nil || !ok || !validRef(params) || !validRef(frozen.Context) ||
		!digestPattern.MatchString(frozen.SnapshotDigest) ||
		!digestPattern.MatchString(frozen.PolicySHA) {
		return SessionPlanClosure{}, ErrInvalid
	}
	descriptors, err := CompileSessionDescriptors(workflow, frozen.ItemCount)
	if err != nil || len(results) != len(descriptors.Tasks) {
		return SessionPlanClosure{}, ErrInvalid
	}
	entries := []Entry{{Ref: params, Contract: "Params"},
		{Ref: frozen.Context, Contract: "SessionPlanContext"},
		{Ref: policy.Calibration, Contract: "Calibration"}}
	for i, result := range results {
		if result.TaskID != descriptors.Tasks[i].ID || !validRef(result.Document) {
			return SessionPlanClosure{}, ErrInvalid
		}
		entries = append(entries, Entry{Ref: result.Document, Contract: "DescriptorShard"})
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	refs := make([]Ref, len(entries))
	for i, entry := range entries {
		if i > 0 && entry.Ref.ArtifactID == entries[i-1].Ref.ArtifactID {
			return SessionPlanClosure{}, ErrInvalid
		}
		refs[i] = entry.Ref
	}
	digest, err := canonical.Hash(struct {
		Version          string `json:"version"`
		Operation        string `json:"operation"`
		OperationVersion string `json:"operation_version"`
		Renderer         string `json:"renderer"`
		SnapshotDigest   string `json:"snapshot_digest"`
		PolicySHA        string `json:"policy_sha"`
		Params           Ref    `json:"params"`
		Inputs           []Ref  `json:"inputs"`
	}{"task_input_v2", "session.plan", "1", "v2.2",
		frozen.SnapshotDigest, frozen.PolicySHA, params, refs})
	if err != nil {
		return SessionPlanClosure{}, err
	}
	outputBytes := int64(frozen.ItemCount) * 16 * 1024
	if outputBytes < 8<<20 {
		outputBytes = 8 << 20
	}
	profile, err := canonical.Marshal(map[string]any{
		"backend": "cpu", "renderer_version": "v2.2",
		"resource_estimate": map[string]int64{
			"ram_bytes": 512 << 20, "vram_bytes": 0,
			"scratch_bytes": 32 << 20, "output_bytes": outputBytes,
			"pixel_count": 0,
		},
	})
	if err != nil {
		return SessionPlanClosure{}, fmt.Errorf("%w: profile", ErrInvalid)
	}
	return SessionPlanClosure{Open: func(context.Context) (Cursor, error) {
		return &sessionEntryCursor{entries: entries}, nil
	}, InputDigest: digest, Profile: profile}, nil
}
