package rawdevelopment

import (
	"context"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

func rawDeploymentFixture() DeploymentConfig {
	_, expected := rawCapabilityFixture()
	return DeploymentConfig{SchemaVersion: 1, ReportID: uuid.New(), ReportSHA: strings.Repeat("a", 64), DecoderVersion: "rawpy-0.27.1/libraw-0.21.4",
		Locks: v2qualification.DeploymentLocks{Operation: "asset.decode", Version: "1", Backend: "cpu", ModelLockSHA: expected.ModelLockSHA, RuntimeLockSHA: expected.RuntimeLockSHA, DeviceProfileSHA: expected.DeviceProfileSHA, DatasetSHA: expected.DatasetSHA, IssuerID: expected.IssuerID.String(), RequiredChecks: append([]string(nil), requiredChecks...), CalibrationSHAs: []string{}, TestFixtureSHAs: []string{strings.Repeat("e", 64)}, EvidenceSHAs: []string{strings.Repeat("f", 64)}, LicenseCleared: true}}
}
func TestRAWDeploymentConfigStrictPins(t *testing.T) {
	for _, mode := range []string{"valid", "unknown", "duplicate", "null", "missing", "backend", "operation", "version", "decoder", "digest", "license", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			config := rawDeploymentFixture()
			switch mode {
			case "backend":
				config.Locks.Backend = "mps"
			case "operation":
				config.Locks.Operation = "mask.prompt"
			case "version":
				config.Locks.Version = "2"
			case "decoder":
				config.DecoderVersion = "latest"
			case "digest":
				config.ReportSHA = "invalid"
			case "license":
				config.Locks.LicenseCleared = false
			}
			raw, err := canonical.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "unknown":
				raw = []byte(strings.TrimSuffix(string(raw), "}") + `,"extra":true}`)
			case "duplicate":
				raw = []byte(strings.TrimSuffix(string(raw), "}") + `,"schema_version":1}`)
			case "null":
				raw = []byte(strings.Replace(string(raw), `"schema_version":1`, `"schema_version":null`, 1))
			case "missing":
				raw = []byte(strings.Replace(string(raw), `,"schema_version":1`, "", 1))
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			source, err := DecodeSourceConfig(ctx, raw)
			if mode == "valid" {
				if err != nil || source.ReportID != config.ReportID || source.DecoderVersion != config.DecoderVersion || source.Objects != nil || source.Bucket != "" {
					t.Fatal("invalid deployment decode", err)
				}
			} else if err == nil {
				t.Fatal("unsafe config accepted")
			}
		})
	}
}
func TestRAWDeploymentFileRestrictions(t *testing.T) {
	raw, err := canonical.Marshal(rawDeploymentFixture())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "raw.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadSourceConfig(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadSourceConfig(context.Background(), "relative.json"); err == nil {
		t.Fatal("relative path accepted")
	}
	link := filepath.Join(dir, "link.json")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadSourceConfig(context.Background(), link); err == nil {
		t.Fatal("symlink accepted")
	}
	if err = os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadSourceConfig(context.Background(), path); err == nil {
		t.Fatal("writable policy accepted")
	}
}
