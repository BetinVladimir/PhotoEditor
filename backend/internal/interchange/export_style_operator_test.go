package interchange

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type exportOperatorBackgroundFactoryStub struct{}

func (exportOperatorBackgroundFactoryStub) BuildExportStyleBackgroundAuthority(context.Context,
	CreateIntent, FrozenExportDocuments, ExportDocumentCatalog, ImportDocumentObjects,
	NativeExportImageQualifier,
) (ExportStyleBackgroundAuthority, error) {
	return &exportBackgroundAuthorityStub{}, nil
}

func TestExportStyleOperatorPinsPoliciesBeforeNativeBuild(t *testing.T) {
	root := t.TempDir()
	sha := func(value string) string { return rawSHA([]byte(value)) }
	write := func(name string, value any) exportOperatorPinnedFile {
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return exportOperatorPinnedFile{Path: path, SHA: rawSHA(raw)}
	}
	locks := func(operation string) v2qualification.DeploymentLocks {
		backend := "mps"
		if strings.HasPrefix(operation, "pair.") {
			backend = "cpu"
		}
		return v2qualification.DeploymentLocks{
			Operation: operation, Version: "1", Backend: backend,
			ModelLockSHA: sha(operation), RuntimeLockSHA: sha("runtime"),
			DeviceProfileSHA: sha("device"), DatasetSHA: sha("dataset"),
			IssuerID: uuid.NewString(), RequiredChecks: []string{"coverage"},
			CalibrationSHAs: []string{sha("calibration")},
			TestFixtureSHAs: []string{sha("fixture")},
			EvidenceSHAs:    []string{sha("evidence")}, LicenseCleared: true,
		}
	}
	report := func(operation string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha(operation + " report")}
	}
	policies := []ExportBodyDependencyPolicyFileEntry{}
	for _, operation := range []string{"pose.detect", "face.landmarks", "pair.fit.body"} {
		policies = append(policies, ExportBodyDependencyPolicyFileEntry{
			Report: report(operation), Locks: locks(operation),
		})
	}
	wire := exportStyleOperatorConfig{
		Schema:       "interchange-export-style-operator/v1",
		Person:       write("person.json", ExportStatsPersonPolicyConfig{Schema: 1, Report: report("person"), Locks: locks("mask.person")}),
		Prompt:       write("prompt.json", ExportPromptMaskPolicyConfig{Schema: 1, Report: report("prompt"), Locks: locks("mask.prompt")}),
		Dependencies: write("dependencies.json", ExportBodyDependencyPolicyFile{Schema: 1, Policies: policies}),
		Alignment:    write("alignment.json", ExportAlignmentPolicyConfig{Schema: 1, Report: report("alignment"), Locks: locks("pair.register")}),
		Detector:     write("detector.json", ExportDetectorPolicyConfig{Schema: 1, Report: report("detector"), Locks: locks("face.detect")}),
		ICCSHA:       sha("icc"), ModelSHA: sha("model"), StyleSHA: sha("style"),
		BodySHA: sha("body"), FitSHA: sha("fit"), DependencySHA: sha("deps"),
		RegistrationSHA: sha("registration"), MaskOriginSHA: sha("mask origin"),
		MaskArraySHA: sha("mask array"), DetectorSHA: sha("detector runtime"),
		Models: map[string]string{},
	}
	base := ExportDocumentValidatorInstallation{
		ICCSHA: wire.ICCSHA, ModelSHA: wire.ModelSHA,
		Other: exportReaderValidator{&importValidatorStub{}}, Semantics: &exportSemanticsStub{},
	}
	manifest := write("operator.json", wire)
	authority, err := LoadExportStyleOperatorAuthority(context.Background(), manifest.Path,
		manifest.SHA, base, exportOperatorBackgroundFactoryStub{})
	if err != nil || authority.pinnedPolicies == nil {
		t.Fatal("pinned operator unavailable", err)
	}
	if _, err := LoadExportStyleOperatorAuthority(context.Background(), manifest.Path,
		sha("wrong"), base, exportOperatorBackgroundFactoryStub{}); err == nil {
		t.Fatal("foreign manifest pin accepted")
	}
	if _, err := LoadExportStyleOperatorAuthority(context.Background(), manifest.Path,
		manifest.SHA, base, nil); err == nil {
		t.Fatal("missing background authority accepted")
	}
	if err := os.WriteFile(wire.Person.Path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExportStyleOperatorAuthority(context.Background(), manifest.Path,
		manifest.SHA, base, exportOperatorBackgroundFactoryStub{}); err == nil {
		t.Fatal("changed subordinate policy accepted")
	}
	scratch, err := NewExportArchiveScratch(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	native := ExportNativeResolverInstallation{
		Scratch: scratch, ImageProbe: PythonExportNativeProbe{
			Python: "/usr/bin/python3", WorkerRoot: root, Timeout: time.Second,
		}, MaskProbe: &exportStyleProbeStub{}, ArrayProbe: &exportStyleProbeStub{},
		ArrayRuntimeSHA: sha("array"),
	}
	validator, err := authority.BuildNativeExportAuthority(context.Background(), native)
	if err != nil || validator == nil {
		t.Fatal("owned policies changed after file mutation", err)
	}
}
