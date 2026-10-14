package interchange

import (
	"context"
	"path/filepath"

	"photoeditor/backend/internal/v2input"
)

// ImportSchedulerOperatorFiles is the process-owned V30 installation. The
// archive and workflow may select neither executable configuration nor model
// qualification files. All required paths are paired with exact SHA pins.
type ImportSchedulerOperatorFiles struct {
	NativePath, NativeSHA string
	StylePath, StyleSHA   string
	Recipes               ImportRecipeDeploymentFiles
	Limits                ImportPlanLimits
}

func (f ImportSchedulerOperatorFiles) ValidateDeclaration() error {
	if f.Limits.ArchiveBytes < 1 || f.Limits.ArchiveBytes > 64<<30 ||
		f.Limits.ExpandedBytes < 1 || f.Limits.ExpandedBytes > 128<<30 ||
		f.Limits.Files < 1 || f.Limits.Files > 30000 {
		return v2input.ErrInvalid
	}
	for _, pair := range [][2]string{
		{f.NativePath, f.NativeSHA}, {f.StylePath, f.StyleSHA},
		{f.Recipes.Restoration.DenoiseFile, f.Recipes.Restoration.DenoiseSHA},
		{f.Recipes.Restoration.UpscaleFile, f.Recipes.Restoration.UpscaleSHA},
		{f.Recipes.Stats.Path, f.Recipes.Stats.SHA},
		{f.Recipes.Person.Path, f.Recipes.Person.SHA},
	} {
		if !filepath.IsAbs(pair[0]) || filepath.Clean(pair[0]) != pair[0] || !validImportRefForSHA(pair[1]) {
			return v2input.ErrInvalid
		}
	}
	return nil
}

func (f ImportSchedulerOperatorFiles) Install(ctx context.Context, base SQLImportEntryRecipeAuthorityFactory, personAcceptance ImportMaskQualifier) (NativeImportSchedulerFactory, error) {
	var zero NativeImportSchedulerFactory
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if personAcceptance == nil || base.StyleInstallation != nil || base.PersonMaskQualification != nil ||
		base.Codecs.OverrideContent == nil {
		return zero, v2input.ErrInvalid
	}
	// Known Params contracts are handled by typed branches of the recipe graph.
	// The process owns the terminal behavior for an unknown contract; an archive
	// must never supply its own executable fallback or make one publish bytes.
	if base.Codecs.Params == nil {
		base.Codecs.Params = RejectUnsupportedImportDependency{}
	}
	if err := f.ValidateDeclaration(); err != nil {
		return zero, err
	}
	// Load before composition; no partially installed factory escapes on a
	// missing file, invalid digest, duplicate policy, or cancelled request.
	native, err := LoadNativeImportDeployment(ctx, f.NativePath, f.NativeSHA)
	if err != nil {
		return zero, err
	}
	style, err := LoadImportNativeStyleInstallation(ctx, f.StylePath, f.StyleSHA)
	if err != nil {
		return zero, err
	}
	base.StyleInstallation = &style
	installed, err := f.Recipes.Install(ctx, base, personAcceptance)
	if err != nil {
		return zero, err
	}
	if err := installed.ValidateImportRecipeInstallation(ctx); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return NativeImportSchedulerFactory{
		Deployment: native,
		Recipes:    SQLImportSchedulerRecipeFactory{Authority: installed},
		Limits:     f.Limits,
	}, nil
}
