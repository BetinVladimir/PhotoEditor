package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	migrations "photoeditor/backend/db"
	"photoeditor/backend/internal/canonical"
)

func lineageReferenceFixture(t *testing.T) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "references.json")
	value := map[string]any{"schema_version": 1, "coverage": migrations.LineageObservationCoverage, "snapshots": []any{map[string]any{"identity": "independently-reviewed-test-snapshot", "semantic_sha256": strings.Repeat("a", 64)}}}
	return path, writeLineageManifest(t, path, value)
}
func writeLineageManifest(t *testing.T, path string, value any) string {
	t.Helper()
	raw, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func TestObserveLineageArgumentsNoApprovalFlagsOrUnpairedReferences(t *testing.T) {
	output := filepath.Join(t.TempDir(), "observation.json")
	args := []string{"observe-lineage", "--output", output}
	if _, err := parseObserveLineageArgs(args); err != nil {
		t.Fatal(err)
	}
	path, pin := lineageReferenceFixture(t)
	if _, err := parseObserveLineageArgs(append(append([]string{}, args...), "--references", path, "--references-sha256", pin)); err != nil {
		t.Fatal(err)
	}
	for _, extra := range [][]string{{"--references", path}, {"--references-sha256", pin}, {"--references", "", "--references-sha256", ""}, {"--references", path, "--references-sha256", strings.Repeat("A", 64)}, {"--approve"}, {"--accept-unknown"}, {"--install"}, {"trailing"}} {
		if _, err := parseObserveLineageArgs(append(append([]string{}, args...), extra...)); err == nil {
			t.Fatal("unsafe flags accepted", extra)
		}
	}
	if _, err := parseObserveLineageArgs([]string{"observe-lineage", "--output", "relative.json"}); err == nil {
		t.Fatal("relative output accepted")
	}
}
func TestObserveLineageReferenceManifestIsIndependentClosedAndPinned(t *testing.T) {
	path, pin := lineageReferenceFixture(t)
	references, err := readLineageReferences(path, pin)
	if err != nil || len(references) != 1 || references[0].SemanticSHA256 != strings.Repeat("a", 64) {
		t.Fatal("valid reference changed", err)
	}
	if _, err := readLineageReferences(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong reference pin accepted")
	}
	for _, damage := range []string{"schema", "coverage", "empty", "extra", "snapshotextra", "duplicateid", "duplicatehash", "badsha"} {
		t.Run(damage, func(t *testing.T) {
			one := map[string]any{"identity": "reviewed", "semantic_sha256": strings.Repeat("a", 64)}
			value := map[string]any{"schema_version": 1, "coverage": migrations.LineageObservationCoverage, "snapshots": []any{one}}
			switch damage {
			case "schema":
				value["schema_version"] = 2
			case "coverage":
				value["coverage"] = "partial"
			case "empty":
				value["snapshots"] = []any{}
			case "extra":
				value["installation_authorized"] = true
			case "snapshotextra":
				one["approved"] = true
			case "duplicateid":
				value["snapshots"] = []any{one, map[string]any{"identity": "reviewed", "semantic_sha256": strings.Repeat("b", 64)}}
			case "duplicatehash":
				value["snapshots"] = []any{one, map[string]any{"identity": "another", "semantic_sha256": strings.Repeat("a", 64)}}
			case "badsha":
				one["semantic_sha256"] = "wrong"
			}
			pin := writeLineageManifest(t, path, value)
			if _, err := readLineageReferences(path, pin); err == nil {
				t.Fatal("invalid reference accepted", damage)
			}
		})
	}
}
func TestObserveLineageReferenceFileBoundsCanonicalAndSymlink(t *testing.T) {
	path, pin := lineageReferenceFixture(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(path), "link.json")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = readLineageReferences(link, pin); err == nil {
		t.Fatal("reference symlink accepted")
	}
	for _, body := range [][]byte{{}, []byte("x"), append(raw, ' '), bytes.Repeat([]byte("x"), (1<<20)+1)} {
		if err = os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(body)
		if _, err = readLineageReferences(path, hex.EncodeToString(hash[:])); err == nil {
			t.Fatal("unsafe reference bytes accepted")
		}
	}
}
func TestObserveLineageOutputExclusivePrivateCanonicalAndNeverAuthorizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observation.json")
	value := migrations.LineageObservation{Format: migrations.LineageObservationFormat, Coverage: migrations.LineageObservationCoverage, ExternalDependencyValidationRequired: true, SemanticSHA256: strings.Repeat("a", 64), ExactLedgerSHA256: strings.Repeat("b", 64)}
	var summary bytes.Buffer
	if err := finishLineageObservation(path, value, nil, &summary); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("output not private", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, canonicalBody) {
		t.Fatal("output not canonical", err)
	}
	var receipt map[string]any
	if err = json.Unmarshal(summary.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["installation_authorized"] != false || receipt["external_dependency_validation_required"] != true || receipt["classification"] != "CAPTURE_ONLY_NOT_CLASSIFIED" {
		t.Fatal("capture promoted to authority", receipt)
	}
	if err := writeExclusiveLineageObservation(path, migrations.LineageObservation{}); err == nil {
		t.Fatal("existing observation overwritten")
	}
	unchanged, _ := os.ReadFile(path)
	if !bytes.Equal(raw, unchanged) {
		t.Fatal("existing evidence changed")
	}
	link := filepath.Join(filepath.Dir(path), "symlink.json")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := writeExclusiveLineageObservation(link, value); err == nil {
		t.Fatal("output symlink followed")
	}
}
func TestObserveLineageClassificationMismatchWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observation.json")
	var output bytes.Buffer
	err := finishLineageObservation(path, migrations.LineageObservation{}, []migrations.LineageSnapshotAuthority{{Identity: "independent", SemanticSHA256: strings.Repeat("a", 64)}}, &output)
	if err == nil {
		t.Fatal("partial unknown observation classified")
	}
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("classification refusal wrote output")
	}
	if output.Len() != 0 {
		t.Fatal("classification refusal emitted success")
	}
}
func TestObserveLineageValidatesReferenceAndOutputBeforeDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	path := filepath.Join(t.TempDir(), "observation.json")
	command := []string{"observe-lineage", "--output", path}
	if err := run(context.Background(), command, &bytes.Buffer{}); err == nil || err.Error() != "DATABASE_URL required" {
		t.Fatal("capture did not reach read-only database boundary", err)
	}
	refs, pin := lineageReferenceFixture(t)
	command = append(command, "--references", refs, "--references-sha256", pin)
	if err := run(context.Background(), command, &bytes.Buffer{}); err == nil || err.Error() != "DATABASE_URL required" {
		t.Fatal("valid independent manifest rejected before DB", err)
	}
	command[len(command)-1] = strings.Repeat("0", 64)
	if err := run(context.Background(), command, &bytes.Buffer{}); err == nil || err.Error() == "DATABASE_URL required" {
		t.Fatal("bad reference reached DB", err)
	}
	if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseObserveLineageArgs([]string{"observe-lineage", "--output", path}); err == nil {
		t.Fatal("existing output accepted before DB")
	}
}
