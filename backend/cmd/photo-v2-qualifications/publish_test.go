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
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
)

func publishFixture(t *testing.T) ([]byte, string, v2qualification.Expected, v2qualification.DeploymentLocks) {
	t.Helper()
	locks := v2qualification.DeploymentLocks{Operation: "render.compose", Version: "1", Backend: "cpu", ModelLockSHA: strings.Repeat("a", 64), RuntimeLockSHA: strings.Repeat("b", 64), DeviceProfileSHA: strings.Repeat("c", 64), DatasetSHA: strings.Repeat("d", 64), IssuerID: uuid.NewString(), RequiredChecks: []string{"quality"}, CalibrationSHAs: []string{strings.Repeat("0", 64)}, TestFixtureSHAs: []string{strings.Repeat("e", 64)}, EvidenceSHAs: []string{strings.Repeat("f", 64)}, LicenseCleared: true}
	expected, err := locks.Expected()
	if err != nil {
		t.Fatal(err)
	}
	report := v2qualification.Report{SchemaVersion: 1, Operation: locks.Operation, Version: locks.Version, ModelLockSHA: locks.ModelLockSHA, RuntimeLockSHA: locks.RuntimeLockSHA, DeviceProfileSHA: locks.DeviceProfileSHA, DatasetSHA: locks.DatasetSHA, InputFixtureSHAs: locks.TestFixtureSHAs, Checks: []v2qualification.Check{{ID: "quality", Passed: true, EvidenceSHA: locks.EvidenceSHAs[0]}}, Metrics: v2qualification.Metrics{P50MS: 1, P95MS: 2, PeakVRAMBytes: 0, PeakRSSBytes: 1024, ScratchBytes: 2048}, Status: "passed", IssuerID: uuid.MustParse(locks.IssuerID), CreatedAt: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = canonical.Bytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return raw, hex.EncodeToString(digest[:]), expected, locks
}
func publishCommand(path, locks, pin string) []string {
	return []string{"publish", "--tenant", uuid.NewString(), "--project", uuid.NewString(), "--operator", uuid.NewString(), "--report-file", path, "--sha256", pin, "--locks", locks, "--idempotency-key", "operator-run-001"}
}
func TestPublishArgumentsAreExplicitAndClosed(t *testing.T) {
	args := publishCommand("/reports/original.json", "/trusted/locks.json", strings.Repeat("a", 64))
	parsed, err := parsePublishArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.key != "operator-run-001" || parsed.report != uuid.Nil || parsed.file != "/reports/original.json" {
		t.Fatal("scope changed")
	}
	for _, change := range [][]string{{"--skip-tests"}, {"--passed"}, {"--backend", "cuda"}, {"trailing"}} {
		if _, err := parsePublishArgs(append(append([]string{}, args...), change...)); err == nil {
			t.Fatal("untrusted override accepted", change)
		}
	}
	for flag, value := range map[string]string{"--tenant": uuid.Nil.String(), "--project": "invalid", "--operator": "", "--report-file": "relative.json", "--locks": "relative.json", "--sha256": strings.Repeat("A", 64), "--idempotency-key": " "} {
		changed := append([]string{}, args...)
		for i := range changed {
			if changed[i] == flag {
				changed[i+1] = value
				break
			}
		}
		if _, err := parsePublishArgs(changed); err == nil {
			t.Fatal("invalid input accepted", flag)
		}
	}
}
func TestPublishOriginalBytesRequireTrustedPolicyAndExactPin(t *testing.T) {
	raw, pin, expected, _ := publishFixture(t)
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readPublishReport(path, pin, expected)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("original report changed", err)
	}
	other := expected
	other.RuntimeLockSHA = strings.Repeat("1", 64)
	if _, err := readPublishReport(path, pin, other); err == nil {
		t.Fatal("report supplied its own runtime authority")
	}
	if _, err := readPublishReport(path, strings.Repeat("0", 64), expected); err == nil {
		t.Fatal("wrong digest accepted")
	}
	if err := os.WriteFile(path, append(raw, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(append(raw, ' '))
	if _, err := readPublishReport(path, hex.EncodeToString(hash[:]), expected); err == nil {
		t.Fatal("noncanonical report accepted")
	}
}
func TestPublishFileBoundsAndSymlinks(t *testing.T) {
	raw, pin, expected, _ := publishFixture(t)
	root := t.TempDir()
	path := filepath.Join(root, "report.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPublishReport(link, pin, expected); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := readPublishReport(root, pin, expected); err == nil {
		t.Fatal("directory accepted")
	}
	for _, body := range [][]byte{{}, []byte("x"), bytes.Repeat([]byte("x"), 8*1024*1024+1)} {
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readPublishReport(path, pin, expected); err == nil {
			t.Fatal("invalid report bounds accepted")
		}
	}
}
func TestPublishLoadsIndependentLocksAndRejectsBeforeDatabase(t *testing.T) {
	raw, pin, _, locks := publishFixture(t)
	root := t.TempDir()
	report := filepath.Join(root, "report.json")
	lockPath := filepath.Join(root, "locks.json")
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(locks)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(lockPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "")
	args := publishCommand(report, lockPath, pin)
	if err := run(context.Background(), args, &bytes.Buffer{}); err == nil || err.Error() != "DATABASE_URL required" {
		t.Fatal("valid file/locks did not reach normal database boundary", err)
	}
	locks.RuntimeLockSHA = strings.Repeat("1", 64)
	body, _ = json.Marshal(locks)
	if err = os.WriteFile(lockPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), args, &bytes.Buffer{}); err == nil || err.Error() == "DATABASE_URL required" {
		t.Fatal("mismatched independent policy reached database", err)
	}
}
