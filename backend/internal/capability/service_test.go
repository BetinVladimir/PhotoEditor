package capability

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestCapabilityStatusSeparatesEvidenceFromSupport(t *testing.T) {
	cases := []struct {
		name, wantStatus, wantReason string
		supported, available         []string
	}{
		{"active qualified worker", "ready", "", []string{"cpu", "cuda"}, []string{"cuda"}},
		{"supported without worker", "unqualified", "no_qualified_worker", []string{"cpu"}, nil},
		{"remote CUDA on Mac client", "ready", "", []string{"cuda"}, []string{"cuda"}},
		{"MPS cannot satisfy CUDA-only", "unqualified", "invalid_backend_evidence", []string{"cuda"}, []string{"mps"}},
		{"missing support with evidence", "unqualified", "invalid_backend_evidence", nil, []string{"cuda"}},
		{"empty backend evidence", "unqualified", "invalid_backend_evidence", []string{"cpu"}, []string{""}},
		{"duplicate supported backend", "unqualified", "invalid_backend_evidence", []string{"mps", "mps"}, []string{"mps"}},
		{"empty supported backend", "unqualified", "invalid_backend_evidence", []string{""}, nil},
		{"duplicate evidence", "unqualified", "invalid_backend_evidence", []string{"cpu"}, []string{"cpu", "cpu"}},
		{"Metal with qualified worker", "ready", "", []string{"mps", "cpu"}, []string{"mps"}},
		{"no backend", "unsupported", "no_supported_backend", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, reason := classifyBackends(tc.supported, tc.available)
			if status != tc.wantStatus {
				t.Fatalf("status=%q, want %q", status, tc.wantStatus)
			}
			if tc.wantReason == "" && reason != nil {
				t.Fatalf("unexpected reason: %s", *reason)
			}
			if tc.wantReason != "" && (reason == nil || *reason != tc.wantReason) {
				t.Fatalf("reason=%v, want %s", reason, tc.wantReason)
			}
		})
	}
}

func TestRetouchInstallationCannotPromoteUnqualifiedWorker(t *testing.T) {
	tenant := uuid.New()
	for _, operation := range []string{"semantic.preview_retouch_region", "semantic.adopt_shown_retouch_region"} {
		reason := "no_qualified_worker"
		original := Operation{Operation: operation, Available: false, Status: "unqualified", Reason: &reason,
			SupportedBackends: json.RawMessage(`["cpu"]`), AvailableBackends: json.RawMessage(`[]`)}
		result := maskSemanticInstallation(original, tenant, func(tid uuid.UUID, op string) bool { return tid == tenant && op == operation })
		if result.Available || result.Status != original.Status || result.Reason == nil || *result.Reason != reason || string(result.AvailableBackends) != `[]` {
			t.Fatal("API installation promoted missing worker qualification", result)
		}
	}
}

func TestSemanticReadinessRequiresTenantAPIInstallation(t *testing.T) {
	tenant, other := uuid.New(), uuid.New()
	for _, operation := range []string{"semantic.preview_local_exposure", "semantic.preview_remove_object", "semantic.preview_background_cleanup", "semantic.preview_retouch_region", "semantic.adopt_shown_exposure", "semantic.adopt_shown_removal", "semantic.adopt_shown_background_cleanup", "semantic.adopt_shown_retouch_region"} {
		original := Operation{Operation: operation, Available: true, Status: "ready", AvailableBackends: json.RawMessage(`["cpu"]`), SupportedBackends: json.RawMessage(`["cpu"]`)}
		for _, installed := range []func(uuid.UUID, string) bool{nil, func(tid uuid.UUID, op string) bool { return tid == other }, func(tid uuid.UUID, op string) bool { return tid == tenant && op == operation }} {
			result := maskSemanticInstallation(original, tenant, installed)
			ready := installed != nil && installed(tenant, operation)
			if result.Available != ready {
				t.Fatal(operation, result)
			}
			if !ready && (result.Status != "unqualified" || string(result.AvailableBackends) != "[]" || result.Reason == nil || *result.Reason != "api_installation_missing") {
				t.Fatal(result)
			}
			if string(result.SupportedBackends) != `["cpu"]` {
				t.Fatal("support changed")
			}
		}
	}
	legacy := Operation{Operation: "asset.verify", Available: true, Status: "ready"}
	if !maskSemanticInstallation(legacy, tenant, nil).Available {
		t.Fatal("legacy availability changed")
	}
}

func TestRemovalInstallationCannotPromoteUnqualifiedWorker(t *testing.T) {
	tenant := uuid.New()
	for _, operation := range []string{"semantic.preview_remove_object", "semantic.adopt_shown_removal"} {
		reason := "no_qualified_worker"
		original := Operation{Operation: operation, Available: false, Status: "unqualified", Reason: &reason,
			SupportedBackends: json.RawMessage(`["cpu"]`), AvailableBackends: json.RawMessage(`[]`)}
		result := maskSemanticInstallation(original, tenant, func(tid uuid.UUID, op string) bool { return tid == tenant && op == operation })
		if result.Available || result.Status != original.Status || result.Reason == nil || *result.Reason != reason || string(result.AvailableBackends) != `[]` {
			t.Fatal("API installation promoted missing worker qualification", result)
		}
	}
}


func TestInvalidBackendEvidenceDisablesOnlyThatOperation(t *testing.T) {
 for _, tc := range []struct{ supported, available []string }{
  {[]string{"cpu","webgpu"},[]string{"webgpu"}},
  {[]string{"cuda"},[]string{"mps"}},
  {[]string{"mps","mps","cpu"},[]string{"mps"}},
 } {
  supported,_:=json.Marshal(tc.supported);available,_:=json.Marshal(tc.available)
  item:=normalizeBackendEvidence(Operation{Operation:"bad",SupportedBackends:supported,AvailableBackends:available},tc.supported,tc.available)
  if item.Available||item.Status!="unqualified"||string(item.AvailableBackends)!="[]"{t.Fatal(item)}
  var clean []string
  if err:=json.Unmarshal(item.SupportedBackends,&clean);err!=nil{t.Fatal(err)}
  for _, backend:=range clean{if !knownBackend(backend){t.Fatal("unknown advertised backend",backend)}}
 }
 // A separately qualified Metal or CPU alternative retains its actual backend.
 for _, backend:=range []string{"mps","cpu","cuda"}{
  item:=normalizeBackendEvidence(Operation{AvailableBackends:json.RawMessage(`["`+backend+`"]`)},[]string{backend},[]string{backend})
  if !item.Available||item.Status!="ready"{t.Fatal(item)}
 }
}

func TestRawBackendShapesFailLocallyWithoutFabricatedReadiness(t *testing.T) {
 for _, tc := range []struct{ supported, available, clean string }{
  {`["cpu","mps"]`,`{"mps":true}`,`["cpu","mps"]`},
  {`["mps",false,"cpu",null,"mps"]`,`["mps"]`,`["mps","cpu"]`},
  {`null`,`["cpu"]`,`[]`},
  {`["cuda"]`,`null`,`["cuda"]`},
  {`{"cuda":true}`,`["cuda"]`,`[]`},
  {`["cpu"]`,`[false,"cpu"]`,`["cpu"]`},
  {`["cpu"]`,`["cpu"`, `["cpu"]`},
 } {
  item:=normalizeRawBackendEvidence(Operation{Operation:"broken",SupportedBackends:json.RawMessage(tc.supported),AvailableBackends:json.RawMessage(tc.available)})
  if item.Available||item.Status!="unqualified"||item.Reason==nil||*item.Reason!="invalid_backend_evidence"||string(item.AvailableBackends)!="[]"||string(item.SupportedBackends)!=tc.clean{t.Fatalf("bad shape was not isolated: %+v",item)}
 }
 good:=normalizeRawBackendEvidence(Operation{SupportedBackends:json.RawMessage(`["cpu","mps"]`),AvailableBackends:json.RawMessage(`["mps"]`)})
 if !good.Available||good.Status!="ready"{t.Fatal("unrelated valid operation lost readiness",good)}
 empty:=normalizeRawBackendEvidence(Operation{SupportedBackends:json.RawMessage(`[]`),AvailableBackends:json.RawMessage(`[]`)})
 if empty.Status!="unsupported"{t.Fatal("valid empty arrays changed semantics",empty)}
}

func TestRawBackendEvidenceBounds(t *testing.T) {
 values:=make([]string,33);for i:=range values{values[i]="cpu"}
 raw,_:=json.Marshal(values)
 for _, value:=range []json.RawMessage{nil,json.RawMessage(`null`),raw,json.RawMessage(make([]byte,32769))}{
  if _,ok:=parseBackendEvidence(value);ok{t.Fatal("invalid or oversized evidence accepted")}
 }
}
