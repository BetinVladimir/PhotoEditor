package delivery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"unicode/utf8"
)

type CreateRequest struct {
	ProjectID  uuid.UUID `json:"project_id"`
	SnapshotID uuid.UUID `json:"snapshot_id"`
	Name       string    `json:"name"`
	Policy     Policy    `json:"policy"`
}

// Required booleans must not silently become false when omitted from JSON.
func (r *CreateRequest) UnmarshalJSON(raw []byte) error {
	type plain CreateRequest
	values, ok := closedJSONObject(raw)
	if !ok {
		return fmt.Errorf("invalid delivery request object")
	}
	for _, name := range []string{"project_id", "snapshot_id", "name", "policy"} {
		if len(values[name]) == 0 || bytes.Equal(bytes.TrimSpace(values[name]), []byte("null")) {
			return fmt.Errorf("missing delivery field %s", name)
		}
	}
	policy, ok := closedJSONObject(values["policy"])
	if !ok {
		return fmt.Errorf("invalid delivery policy object")
	}
	for _, name := range []string{"required_presets", "optional_presets", "require_review", "metadata_policy", "provenance", "allow_omitted"} {
		if len(policy[name]) == 0 || bytes.Equal(bytes.TrimSpace(policy[name]), []byte("null")) {
			return fmt.Errorf("missing delivery policy field %s", name)
		}
	}
	if value, present := policy["approved_fields"]; present && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return fmt.Errorf("null delivery metadata approval")
	}
	for _, name := range []string{"signing_policy_id", "signing_policy_sha"} {
		if value, present := policy[name]; present && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("null delivery signing policy field %s", name)
		}
	}
	var decoded plain
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*r = CreateRequest(decoded)
	return nil
}

type CreateIdentity struct {
	Run, Workflow, Package, PrepareTask uuid.UUID
	FrozenInput, Params, Policy         uuid.UUID
}
type CreateIntent struct {
	key           string
	tenant, actor uuid.UUID
	request       CreateRequest
	identity      CreateIdentity
	requestJCS    []byte
	requestSHA    string
}

// Identity is scoped to tenant+actor+key. Payload conflicts are still enforced
// by the durable idempotency ledger, never by generating a second graph.
func FreezeCreateIntent(tid uuid.UUID, who auth.Identity, request CreateRequest, key string) (CreateIntent, error) {
	invalid := func() (CreateIntent, error) { return CreateIntent{}, domain.NewError("VALIDATION_FAILED", 422) }
	if tid == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || request.ProjectID == uuid.Nil || request.SnapshotID == uuid.Nil || !idempotency.ValidKey(key) || !utf8.ValidString(request.Name) || utf8.RuneCountInString(request.Name) < 1 || utf8.RuneCountInString(request.Name) > 200 {
		return invalid()
	}
	p := request.Policy
	if len(p.RequiredPresets) < 1 || len(p.RequiredPresets) > 16 || p.OptionalPresets == nil || len(p.OptionalPresets) > 16 || len(p.RequiredPresets)+len(p.OptionalPresets) > 16 {
		return invalid()
	}
	seen := map[uuid.UUID]bool{}
	for _, group := range [][]uuid.UUID{p.RequiredPresets, p.OptionalPresets} {
		for _, id := range group {
			if id == uuid.Nil || seen[id] {
				return invalid()
			}
			seen[id] = true
		}
	}
	if p.MetadataPolicy != "strip" && p.MetadataPolicy != "copyright" && p.MetadataPolicy != "approved_exif" || p.Provenance != "off" && p.Provenance != "required" {
		return invalid()
	}
	if !validDeliverySigningPolicyChoice(p) {
		return invalid()
	}
	fields, err := normalizeMetadataApproval(p.MetadataPolicy, p.ApprovedFields)
	if err != nil {
		return invalid()
	}
	request.Policy.ApprovedFields = fields
	raw, err := canonical.Marshal(request)
	if err != nil {
		return invalid()
	}
	var owned CreateRequest
	if json.Unmarshal(raw, &owned) != nil {
		return invalid()
	}
	run := uuid.NewSHA1(tid, []byte("delivery:create:"+who.UserID.String()+":"+key))
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return invalid()
	}
	task, err := v2input.TaskID(workflow, "v2.delivery.prepare", 0)
	if err != nil {
		return invalid()
	}
	ids := CreateIdentity{Run: run, Workflow: workflow, Package: uuid.NewSHA1(workflow, []byte("delivery:package:1")), PrepareTask: task, FrozenInput: uuid.NewSHA1(task, []byte("delivery:frozen-input:1")), Params: uuid.NewSHA1(task, []byte("delivery:params:1")), Policy: uuid.NewSHA1(workflow, []byte("delivery:policy:1"))}
	return CreateIntent{key: key, tenant: tid, actor: who.UserID, request: owned, identity: ids, requestJCS: raw, requestSHA: digest(raw)}, nil
}
func (i CreateIntent) Identity() CreateIdentity { return i.identity }
func (i CreateIntent) RequestJCS() []byte       { return append([]byte(nil), i.requestJCS...) }
func (i CreateIntent) RequestSHA() string       { return i.requestSHA }
func (i CreateIntent) Request() CreateRequest {
	r := i.request
	r.Policy.ApprovedFields = append([]string(nil), r.Policy.ApprovedFields...)
	r.Policy.RequiredPresets = append([]uuid.UUID(nil), r.Policy.RequiredPresets...)
	r.Policy.OptionalPresets = append([]uuid.UUID{}, r.Policy.OptionalPresets...)
	return r
}
