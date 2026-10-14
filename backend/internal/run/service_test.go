package run

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

func TestValidateJobOverride(t *testing.T) {
	if err := validateJobOverride(json.RawMessage(`{"schema_version":1,"modules":{"strength":0.4}}`)); err != nil {
		t.Fatalf("valid job override rejected: %v", err)
	}
	for _, raw := range []string{
		`{"schema_version":1,"modules":{}}`,
		`{"schema_version":1,"modules":{"model_path":"/tmp/model"}}`,
		`{"schema_version":1,"modules":{"strength":0.4},"extra":true}`,
	} {
		if err := validateJobOverride(json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid job override accepted: %s", raw)
		}
	}
}

func TestAnalysisApplyBindsOverridesToExactSelectedVersions(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	versionA, versionB := uuid.New(), uuid.New()
	shaA, shaB := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	makeBody := func(sha string) json.RawMessage {
		body, err := canonical.Marshal(map[string]any{
			"schema_version": 1, "revision": 1, "input_sha256": sha,
			"modules": map[string]any{"global": map[string]any{"saturation": 1}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	items := []applySelection{{ItemID: first, AssetVersionID: versionA, SHA256: shaA},
		{ItemID: second, AssetVersionID: versionB, SHA256: shaB}}
	bound := []BoundItemOverride{{second, versionB, shaB, makeBody(shaB)},
		{first, versionA, shaA, makeBody(shaA)}}
	result, _, err := validateBoundItemOverrides(items, bound)
	if err != nil || len(result) != 2 ||
		(result[0].ItemID != first && result[1].ItemID != first) ||
		(result[0].ItemID != second && result[1].ItemID != second) {
		t.Fatalf("exact selected sources rejected: %+v, %v", result, err)
	}
	wrong := append([]BoundItemOverride(nil), bound...)
	wrong[0].AssetVersionID = versionA
	if _, _, err := validateBoundItemOverrides(items, wrong); err == nil {
		t.Fatal("override accepted for another version")
	}
	wrong = append([]BoundItemOverride(nil), bound...)
	wrong[1] = wrong[0]
	if _, _, err := validateBoundItemOverrides(items, wrong); err == nil {
		t.Fatal("duplicate override accepted")
	}
}

func TestAnalysisGraphStyleOnlyEnablesRequiredBeautyStage(t *testing.T) {
	beauty := json.RawMessage(`{"enabled":true,"algorithm":"beautyfilter_v1","smoothing":0.1,"tone_evenness":0,"texture_keep":1,"skin_mask":"face68_conservative_v1","protect_features":true}`)
	graph, err := analysisGraphStyle(identityStyleJSON, beauty)
	if err != nil || len(graph) == 0 {
		t.Fatalf("beauty graph not compiled: %v", err)
	}
	var original, planned map[string]json.RawMessage
	if json.Unmarshal(identityStyleJSON, &original) != nil ||
		json.Unmarshal(graph, &planned) != nil {
		t.Fatal("invalid graph style")
	}
	canonicalBeauty, err := canonical.Bytes(beauty)
	if err != nil || string(planned["beauty"]) != string(canonicalBeauty) {
		t.Fatal("beauty stage not enabled")
	}
	delete(original, "beauty")
	delete(planned, "beauty")
	baseline, err := canonical.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	withoutBeauty, err := canonical.Marshal(planned)
	if err != nil || string(baseline) != string(withoutBeauty) {
		t.Fatal("non-beauty style fields changed")
	}
}

func TestEmbeddedIdentityStyleIsProductionRecipe(t *testing.T) {
	canonicalBody, err := canonical.Bytes(identityStyleJSON)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Schema  string `json:"schema_version"`
		Mode    string `json:"mode"`
		Quality struct {
			Status         string `json:"status"`
			Transfer       string `json:"transfer_confidence"`
			Reconstruction any    `json:"reconstruction"`
			Validation     any    `json:"validation"`
		} `json:"quality"`
	}
	if err = json.Unmarshal(canonicalBody, &value); err != nil {
		t.Fatal(err)
	}
	if value.Schema != "2.0.0" || value.Mode != "fixed" || value.Quality.Status != "needs_review" ||
		value.Quality.Transfer != "unvalidated" || value.Quality.Reconstruction != nil || value.Quality.Validation != nil {
		t.Fatalf("invalid identity recipe: %+v", value)
	}
}

func TestRetryRejectsInvalidSelectionBeforeDatabase(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	cases := []RetryRequest{{ExpectedRevision: "0"}, {ExpectedRevision: "1", TaskIDs: []uuid.UUID{uuid.Nil}}, {ExpectedRevision: "1", TaskIDs: []uuid.UUID{uuid.New(), uuid.Nil}}}
	for i, req := range cases {
		_, err := s.Retry(context.Background(), who, uuid.New(), uuid.New(), "retry-test", req)
		if domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	id := uuid.New()
	req := RetryRequest{ExpectedRevision: "1", TaskIDs: []uuid.UUID{id, id}}
	if _, err := s.Retry(context.Background(), who, uuid.New(), uuid.New(), "retry-test", req); domain.AsError(err).Code != "VALIDATION_FAILED" {
		t.Fatalf("duplicate: %v", err)
	}
	for _, revision := range []string{"", "01", "+1", "-1", "9223372036854775808"} {
		if _, err := s.Retry(context.Background(), who, uuid.New(), uuid.New(), "retry-test", RetryRequest{ExpectedRevision: revision}); domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("invalid revision %q: %v", revision, err)
		}
	}
}

func TestRetryRequiresIdempotencyKeyBeforeDatabase(t *testing.T) {
	s := Service{}
	_, err := s.Retry(context.Background(), auth.Identity{UserID: uuid.New()}, uuid.New(), uuid.New(), "", RetryRequest{ExpectedRevision: "1"})
	if domain.AsError(err).Code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatal(err)
	}
}

func TestCancelRequiresIdempotencyKeyBeforeDatabase(t *testing.T) {
	s := Service{}
	_, err := s.Cancel(context.Background(), auth.Identity{UserID: uuid.New()}, uuid.New(), uuid.New(), "")
	if domain.AsError(err).Code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatal(err)
	}
}

func TestRetryRevisionPreservesFullInt64Decimal(t *testing.T) {
	for _, revision := range []string{"1", "9007199254740993", "9223372036854775807"} {
		value, err := parseRetryRevision(revision)
		if err != nil || strconv.FormatInt(value, 10) != revision {
			t.Fatalf("valid revision %q lost precision: %v", revision, err)
		}
	}
	for _, revision := range []string{"", "0", "01", "+1", "-1", "9223372036854775808"} {
		if _, err := parseRetryRevision(revision); domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("invalid revision %q accepted: %v", revision, err)
		}
	}
}

func TestApplyRejectsInvalidContractBeforeDatabase(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	strength := 1.0
	valid := ApplyRequest{SourceRevision: 1, StyleVersionID: uuid.New(), Scope: "all", Strength: &strength}
	cases := []ApplyRequest{{}, valid, valid, valid, valid}
	cases[1].SourceRevision = 0
	cases[2].Scope = "selected"
	nan := math.NaN()
	cases[3].Strength = &nan
	cases[4].ItemIDs = []uuid.UUID{uuid.New()}
	for i, req := range cases {
		if _, err := s.Apply(context.Background(), who, uuid.New(), uuid.New(), "key", req); domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("case %d: %v", i, err)
		}
	}
}

func TestRunCreateOnlyAcceptsCompleteApplyDiscriminator(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	if _, err := s.Create(context.Background(), who, uuid.New(), "key", CreateRequest{Kind: "preview", SourceCollectionID: uuid.New()}); domain.AsError(err).Code != "VALIDATION_FAILED" {
		t.Fatalf("unexpected %v", err)
	}
}

func TestPersonModelRequiresExactWeightIdentity(t *testing.T) {
	if !hasPersonModel([]styleModelReference{{ID: personModelID, WeightID: personWeightID}}) {
		t.Fatal("exact qualified person model rejected")
	}
	for _, refs := range [][]styleModelReference{
		{{ID: personModelID, WeightID: "latest"}},
		{{ID: "another_model", WeightID: personWeightID}},
		nil,
	} {
		if hasPersonModel(refs) {
			t.Fatal("non-exact person model accepted")
		}
	}
}
