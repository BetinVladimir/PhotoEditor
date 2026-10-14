package v2region

import (
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// FrozenRegionNode binds Params, resolved inputs and a server-qualified runtime
// profile. It does not grant authority or qualify a renderer. Admission must
// recheck scope in its transaction before persisting this node in run config.
type FrozenRegionNode struct {
	body   []byte
	inputs FrozenRegionInputs
}

func (n FrozenRegionNode) JSON() []byte { return append([]byte(nil), n.body...) }

type regionNodeDocument struct {
	TaskID      uuid.UUID       `json:"task_id"`
	Params      v2input.Ref     `json:"params"`
	ParamsJCS   string          `json:"params_jcs"`
	InputDigest string          `json:"input_digest"`
	Profile     json.RawMessage `json:"profile"`
}

// CompileNode accepts the profile before input-transport decoration. Transport
// keys are rejected because commit compares the undecorated execution profile.
// Additional resource/operator pins are preserved; their qualification belongs
// to the server profile provider, never request-supplied JSON.
func (p FrozenRegionPlan) CompileNode(task uuid.UUID, params v2input.Ref, resolved []v2input.Entry, profile []byte) (FrozenRegionNode, error) {
	if !validRegionSHA(p.digest) || len(p.body) == 0 || len(profile) == 0 || len(profile) > 64*1024 {
		return FrozenRegionNode{}, ErrRegionInvalid
	}
	document, err := p.decode()
	if err != nil {
		return FrozenRegionNode{}, err
	}
	// Canonical parsing rejects duplicate keys instead of silently choosing one.
	profileJCS, err := canonical.Bytes(profile)
	if err != nil {
		return FrozenRegionNode{}, ErrRegionInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(profileJCS, &fields) != nil || fields == nil {
		return FrozenRegionNode{}, ErrRegionInvalid
	}
	for _, key := range []string{"contracts_sha", "v2_input_manifest", "v2_entry_count", "v2_entries_sha"} {
		if _, present := fields[key]; present {
			return FrozenRegionNode{}, ErrRegionInvalid
		}
	}
	value := func(key string) string { var v string; _ = json.Unmarshal(fields[key], &v); return v }
	if value("renderer_version") != "v2.2" || value("backend") != "cuda" ||
		value("icc_lock_sha") != document.FrozenRecipe.ICCLockSHA ||
		value("model_lock_sha") != document.FrozenRecipe.ModelLockSHA ||
		!validRegionSHA(value("region_runtime_sha256")) {
		return FrozenRegionNode{}, ErrRegionInvalid
	}
	if err := p.validatePinnedProfile(fields); err != nil {
		return FrozenRegionNode{}, err
	}
	inputs, err := p.FreezeInputs(task, params, resolved)
	if err != nil {
		return FrozenRegionNode{}, err
	}
	body, err := canonical.Marshal(regionNodeDocument{task, params, string(p.body), inputs.InputDigest, json.RawMessage(profileJCS)})
	if err != nil {
		return FrozenRegionNode{}, ErrRegionInvalid
	}
	return FrozenRegionNode{body, inputs}, nil
}

func validRegionSHA(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

// ConfigJSON returns the v26_region member, to be persisted before sealing the
// graph. Session is live authority, excluded from the content-addressed Params.
func (n FrozenRegionNode) ConfigJSON(session uuid.UUID) ([]byte, error) {
	if session == uuid.Nil || len(n.body) == 0 {
		return nil, ErrRegionInvalid
	}
	return canonical.Marshal(struct {
		Session uuid.UUID         `json:"auth_session_id"`
		Nodes   []json.RawMessage `json:"nodes"`
	}{session, []json.RawMessage{json.RawMessage(n.body)}})
}

// Inputs returns transport factories whose cursor inventory is owned by this
// node. Mutating the returned direct refs cannot change subsequent retries.
func (n FrozenRegionNode) Inputs() FrozenRegionInputs {
	out := n.inputs
	out.Direct = append([]v2input.Ref(nil), out.Direct...)
	return out
}
