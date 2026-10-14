package interchange

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestImportSchedulerOperatorFilesComposeCompletePinnedFactory(t *testing.T) {
	root := t.TempDir()
	write := func(name string, value any) (string, string) {
		t.Helper()
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path, rawSHA(raw)
	}
	nativePath, nativeSHA := write("native.json", nativeImportDeploymentFixture(t))
	sha := func(value string) string { return rawSHA([]byte(value)) }
	stylePath, styleSHA := write("style.json", importNativeStyleDeploymentWire{
		Schema: "interchange-import-style-installation/v1", WorkICCSHA: sha("icc"),
		StyleRuntimeSHA: sha("style"), ImageRuntimeSHA: sha("image"), Models: map[string]string{},
	})
	files := ImportSchedulerOperatorFiles{
		NativePath: nativePath, NativeSHA: nativeSHA, StylePath: stylePath, StyleSHA: styleSHA,
		Recipes: recipeDeploymentFixture(t), Limits: ImportPlanLimits{ArchiveBytes: 1 << 30, ExpandedBytes: 2 << 30, Files: 30000},
	}
	base := SQLImportEntryRecipeAuthorityFactory{
		MaskInstallation: &ImportMaskPolicyInstallation{Manual: true},
		Codecs:           ImportRecipeCodecDependencies{Params: installationOnlyImportPort{}, OverrideContent: installationOnlyImportPort{}},
	}
	installed, err := files.Install(context.Background(), base, &importMaskQualifierStub{})
	if err != nil || installed.Recipes == nil || installed.Limits != files.Limits {
		t.Fatal("complete operator factory unavailable", err)
	}
	if err := installed.Recipes.(SQLImportSchedulerRecipeFactory).ValidateImportRecipeInstallation(context.Background()); err != nil {
		t.Fatal("installed recipe authority invalid", err)
	}
	if base.StyleInstallation != nil || base.PersonMaskQualification != nil {
		t.Fatal("input factory mutated")
	}
	withoutFallback := base
	withoutFallback.Codecs.Params = nil
	withTerminal, err := files.Install(context.Background(), withoutFallback, &importMaskQualifierStub{})
	if err != nil {
		t.Fatal("process-owned unknown Params fallback unavailable", err)
	}
	authority := withTerminal.Recipes.(SQLImportSchedulerRecipeFactory).Authority.(SQLImportEntryRecipeAuthorityFactory)
	if _, ok := authority.Codecs.Params.(RejectUnsupportedImportDependency); !ok || withoutFallback.Codecs.Params != nil {
		t.Fatal("unknown Params fallback was not isolated to installed factory")
	}
	withoutContent := base
	withoutContent.Codecs.OverrideContent = nil
	processOwned, err := files.Install(context.Background(), withoutContent, &importMaskQualifierStub{})
	if err != nil || processOwned.Recipes == nil {
		t.Fatal("process-owned Override content installation unavailable", err)
	}
	files.StyleSHA = sha("foreign")
	if broken, err := files.Install(context.Background(), base, &importMaskQualifierStub{}); err == nil || broken.Recipes != nil {
		t.Fatal("foreign style pin produced a partial factory", err)
	}
}

func TestImportSchedulerOperatorFilesRejectIncompleteBeforeFileIO(t *testing.T) {
	files := ImportSchedulerOperatorFiles{NativePath: "/not-opened", NativeSHA: rawSHA([]byte("native")),
		StylePath: "/not-opened", StyleSHA: rawSHA([]byte("style")),
		Limits: ImportPlanLimits{ArchiveBytes: 1, ExpandedBytes: 1, Files: 1}}
	if _, err := files.Install(context.Background(), SQLImportEntryRecipeAuthorityFactory{}, &importMaskQualifierStub{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("incomplete declaration attempted file IO", err)
	}
}

func TestImportSchedulerOperatorFilesValidateAllPinsAndPortsBeforeIO(t *testing.T) {
	sha := rawSHA([]byte("pin"))
	missing := "/not-opened"
	files := ImportSchedulerOperatorFiles{
		NativePath: missing, NativeSHA: sha, StylePath: missing, StyleSHA: "bad",
		Recipes: ImportRecipeDeploymentFiles{
			Restoration: ImportRestorationDeploymentFiles{DenoiseFile: missing, DenoiseSHA: sha, UpscaleFile: missing, UpscaleSHA: sha},
			Stats:       ImportStatsDeploymentFile{Path: missing, SHA: sha},
			Person:      ImportPersonMaskDeploymentFile{Path: missing, SHA: sha},
		},
		Limits: ImportPlanLimits{ArchiveBytes: 1, ExpandedBytes: 1, Files: 1},
	}
	base := SQLImportEntryRecipeAuthorityFactory{Codecs: ImportRecipeCodecDependencies{Params: installationOnlyImportPort{}, OverrideContent: installationOnlyImportPort{}}}
	if _, err := files.Install(context.Background(), base, &importMaskQualifierStub{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("invalid style pin reached native file IO", err)
	}
	files.StyleSHA = sha
	base.Codecs.OverrideContent = nil
	files.Recipes.Stats.Path = "relative.json"
	if _, err := files.Install(context.Background(), base, &importMaskQualifierStub{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("relative operator path reached native file IO", err)
	}
}
