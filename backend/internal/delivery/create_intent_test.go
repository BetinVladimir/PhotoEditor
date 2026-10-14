package delivery

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"strings"
	"testing"
)

func createRequestFixture() CreateRequest {
	return CreateRequest{ProjectID: uuid.New(), SnapshotID: uuid.New(), Name: "Delivery", Policy: Policy{RequiredPresets: []uuid.UUID{uuid.New()}, OptionalPresets: []uuid.UUID{}, RequireReview: true, MetadataPolicy: "strip", Provenance: "off", AllowOmitted: true}}
}
func TestCreateIntentOwnsPolicyAndStableIdentity(t *testing.T) {
	tid := uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := createRequestFixture()
	intent, err := FreezeCreateIntent(tid, who, request, "key")
	if err != nil {
		t.Fatal(err)
	}
	initial := intent.Request()
	request.Policy.RequiredPresets[0] = uuid.New()
	if intent.Request().Policy.RequiredPresets[0] != initial.Policy.RequiredPresets[0] {
		t.Fatal("borrowed request policy")
	}
	out := intent.Request()
	out.Policy.RequiredPresets[0] = uuid.New()
	raw := intent.RequestJCS()
	raw[0] = 'x'
	if intent.Request().Policy.RequiredPresets[0] != initial.Policy.RequiredPresets[0] || intent.RequestJCS()[0] != '{' {
		t.Fatal("borrowed output")
	}
	changed := initial
	changed.Name = "New payload"
	other, err := FreezeCreateIntent(tid, who, changed, "key")
	if err != nil {
		t.Fatal(err)
	}
	if other.Identity() != intent.Identity() || other.RequestSHA() == intent.RequestSHA() {
		t.Fatal("key scope or payload conflict binding")
	}
	who.SessionID = uuid.New()
	same, err := FreezeCreateIntent(tid, who, initial, "key")
	if err != nil || same.Identity() != intent.Identity() {
		t.Fatal("session rotation changed graph identity", err)
	}
	who.UserID = uuid.New()
	different, err := FreezeCreateIntent(tid, who, initial, "key")
	if err != nil || different.Identity() == intent.Identity() {
		t.Fatal("actor scope collapsed", err)
	}
}
func TestCreateIntentValidation(t *testing.T) {
	tid := uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	for _, mutate := range []func(*CreateRequest){
		func(r *CreateRequest) { r.ProjectID = uuid.Nil }, func(r *CreateRequest) { r.SnapshotID = uuid.Nil },
		func(r *CreateRequest) { r.Name = "" }, func(r *CreateRequest) { r.Name = strings.Repeat("ф", 201) },
		func(r *CreateRequest) { r.Policy.OptionalPresets = nil }, func(r *CreateRequest) { r.Policy.RequiredPresets = nil },
		func(r *CreateRequest) {
			r.Policy.RequiredPresets = append(r.Policy.RequiredPresets, r.Policy.RequiredPresets[0])
		},
		func(r *CreateRequest) { r.Policy.OptionalPresets = []uuid.UUID{r.Policy.RequiredPresets[0]} },
		func(r *CreateRequest) { r.Policy.OptionalPresets = []uuid.UUID{uuid.Nil} },
		func(r *CreateRequest) { r.Policy.MetadataPolicy = "unknown" }, func(r *CreateRequest) { r.Policy.Provenance = "unknown" },
		func(r *CreateRequest) { r.Policy.Provenance = "required" },
	} {
		r := createRequestFixture()
		mutate(&r)
		if _, err := FreezeCreateIntent(tid, who, r, "key"); err == nil {
			t.Fatal("invalid/unavailable request accepted", r)
		}
	}
	for _, key := range []string{"", " key", "key ", strings.Repeat("k", 129)} {
		if _, err := FreezeCreateIntent(tid, who, createRequestFixture(), key); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
}
func TestCreateIntentUsesSixteenPresetTotalLimit(t *testing.T) {
	tid := uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := createRequestFixture()
	request.Policy.RequiredPresets = nil
	for range 16 {
		request.Policy.RequiredPresets = append(request.Policy.RequiredPresets, uuid.New())
	}
	if _, err := FreezeCreateIntent(tid, who, request, "sixteen-required"); err != nil {
		t.Fatal("sixteen required presets refused", err)
	}
	request.Policy.OptionalPresets = append(request.Policy.OptionalPresets, request.Policy.RequiredPresets[15])
	request.Policy.RequiredPresets = request.Policy.RequiredPresets[:15]
	if _, err := FreezeCreateIntent(tid, who, request, "mixed-sixteen"); err != nil {
		t.Fatal("mixed sixteen presets refused", err)
	}
	request.Policy.OptionalPresets = append(request.Policy.OptionalPresets, uuid.New())
	if _, err := FreezeCreateIntent(tid, who, request, "seventeen"); err == nil {
		t.Fatal("seventeen presets accepted")
	}
}
func TestCreateRequestRequiresEveryContractField(t *testing.T) {
	raw, err := json.Marshal(createRequestFixture())
	if err != nil {
		t.Fatal(err)
	}
	var request CreateRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	for _, duplicate := range []string{
		strings.Replace(string(raw), `"name":"Delivery"`, `"name":"Delivery","name":"Altered"`, 1),
		strings.Replace(string(raw), `"provenance":"off"`, `"provenance":"off","provenance":"required"`, 1),
	} {
		if duplicate == string(raw) || json.Unmarshal([]byte(duplicate), &request) == nil {
			t.Fatal("duplicate delivery field accepted")
		}
	}
	for _, field := range []string{"project_id", "snapshot_id", "name", "policy"} {
		var wire map[string]json.RawMessage
		_ = json.Unmarshal(raw, &wire)
		delete(wire, field)
		body, _ := json.Marshal(wire)
		if err := json.Unmarshal(body, &request); err == nil {
			t.Fatal("missing field accepted", field)
		}
	}
	for _, field := range []string{"required_presets", "optional_presets", "require_review", "metadata_policy", "provenance", "allow_omitted"} {
		for _, null := range []bool{false, true} {
			var wire map[string]json.RawMessage
			_ = json.Unmarshal(raw, &wire)
			var policy map[string]json.RawMessage
			_ = json.Unmarshal(wire["policy"], &policy)
			if null {
				policy[field] = json.RawMessage("null")
			} else {
				delete(policy, field)
			}
			wire["policy"], _ = json.Marshal(policy)
			body, _ := json.Marshal(wire)
			if err := json.Unmarshal(body, &request); err == nil {
				t.Fatal("missing/null policy field accepted", field)
			}
		}
	}
	var wire map[string]json.RawMessage
	_ = json.Unmarshal(raw, &wire)
	wire["unknown"] = json.RawMessage("true")
	body, _ := json.Marshal(wire)
	if err := json.Unmarshal(body, &request); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestCreateIntentPinsSigningPolicyChoice(t *testing.T) {
	tid := uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := createRequestFixture()
	request.Policy.SigningPolicyID = uuid.New()
	request.Policy.SigningPolicySHA = strings.Repeat("a", 64)
	if _, err := FreezeCreateIntent(tid, who, request, "signing-policy-choice"); err == nil {
		t.Fatal("off mode accepted hidden signing policy")
	}
	request.Policy.Provenance = "required"
	if intent, err := FreezeCreateIntent(tid, who, request, "signing-policy-choice"); err != nil || intent.Request().Policy.SigningPolicyID != request.Policy.SigningPolicyID {
		t.Fatal("required signing choice not frozen", err)
	}
	request.Policy.SigningPolicySHA = "bad"
	if validDeliverySigningPolicyChoice(request.Policy) {
		t.Fatal("invalid policy digest accepted")
	}
	request.Policy.SigningPolicySHA = strings.Repeat("a", 64)
	request.Policy.SigningPolicyID = uuid.Nil
	if validDeliverySigningPolicyChoice(request.Policy) {
		t.Fatal("missing signing policy accepted")
	}
	raw, err := json.Marshal(createRequestFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"signing_policy_id", "signing_policy_sha"} {
		var envelope map[string]json.RawMessage
		if json.Unmarshal(raw, &envelope) != nil {
			t.Fatal("fixture")
		}
		var body map[string]json.RawMessage
		if json.Unmarshal(envelope["policy"], &body) != nil {
			t.Fatal("fixture policy")
		}
		body[name] = json.RawMessage("null")
		envelope["policy"], _ = json.Marshal(body)
		mutated, _ := json.Marshal(envelope)
		var decoded CreateRequest
		if json.Unmarshal(mutated, &decoded) == nil {
			t.Fatal("null signing field accepted", name)
		}
	}
}

func TestMetadataCreateIntentPinsSortedExplicitConsent(t *testing.T) {
	tid := uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := createRequestFixture()
	request.Policy.MetadataPolicy = "approved_exif"
	request.Policy.ApprovedFields = []string{"gps", "author"}
	intent, err := FreezeCreateIntent(tid, who, request, "metadata")
	if err != nil {
		t.Fatal(err)
	}
	request.Policy.ApprovedFields[0] = "lens"
	owned := intent.Request()
	if len(owned.Policy.ApprovedFields) != 2 || owned.Policy.ApprovedFields[0] != "author" || owned.Policy.ApprovedFields[1] != "gps" {
		t.Fatal("consent lost")
	}
	owned.Policy.ApprovedFields[0] = "copyright"
	if intent.Request().Policy.ApprovedFields[0] != "author" {
		t.Fatal("consent mutable")
	}
	reordered := intent.Request()
	reordered.Policy.ApprovedFields = []string{"gps", "author"}
	other, err := FreezeCreateIntent(tid, who, reordered, "metadata")
	if err != nil || other.RequestSHA() != intent.RequestSHA() {
		t.Fatal("consent order changed request identity", err)
	}
	changed := intent.Request()
	changed.Policy.ApprovedFields = []string{"author"}
	other, err = FreezeCreateIntent(tid, who, changed, "metadata")
	if err != nil || other.RequestSHA() == intent.RequestSHA() {
		t.Fatal("consent change not pinned", err)
	}
}
