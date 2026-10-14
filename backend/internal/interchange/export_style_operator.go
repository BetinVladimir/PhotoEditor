package interchange

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"path/filepath"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// exportStylePinnedPolicies contains validated, owned operator policy values.
// No request or archive controls these values or the files from which they came.
type exportStylePinnedPolicies struct {
	Person       SQLExportStatsPersonPolicyInstallation
	Prompt       SQLExportPromptMaskPolicyInstallation
	Dependencies []ExportBodyDependencyPolicyConfig
	Alignment    ExportAlignmentPolicyDeployment
	Detector     ExportDetectorPolicyDeployment
}

type exportOperatorPinnedFile struct {
	Path string `json:"path"`
	SHA  string `json:"sha256"`
}

type exportStyleOperatorConfig struct {
	Schema             string                   `json:"schema_version"`
	Person             exportOperatorPinnedFile `json:"person"`
	Prompt             exportOperatorPinnedFile `json:"prompt"`
	Dependencies       exportOperatorPinnedFile `json:"dependencies"`
	Alignment          exportOperatorPinnedFile `json:"alignment"`
	Detector           exportOperatorPinnedFile `json:"detector"`
	ICCSHA             string                   `json:"icc_sha256"`
	ModelSHA           string                   `json:"model_sha256"`
	StyleSHA           string                   `json:"style_runtime_sha256"`
	BodySHA            string                   `json:"body_runtime_sha256"`
	FitSHA             string                   `json:"fit_runtime_sha256"`
	DependencySHA      string                   `json:"dependency_runtime_sha256"`
	RegistrationSHA    string                   `json:"registration_runtime_sha256"`
	MaskOriginSHA      string                   `json:"mask_origin_runtime_sha256"`
	MaskArraySHA       string                   `json:"mask_array_runtime_sha256"`
	DetectorSHA        string                   `json:"detector_runtime_sha256"`
	Models             map[string]string        `json:"models"`
	StyleWeights       map[string]string        `json:"style_weights,omitempty"`
	PersonOriginIssuer uuid.UUID                `json:"person_origin_issuer_id"`
	PersonOriginPublic string                   `json:"person_origin_public_key_hex"`
}

// LoadExportStyleOperatorAuthority binds every policy to manifest SHA before
// composing the native factory. Source/model/consent semantics and background
// authority remain independent capabilities; the manifest cannot mint them.
func LoadExportStyleOperatorAuthority(ctx context.Context, path, pin string,
	base ExportDocumentValidatorInstallation, backgrounds ExportStyleBackgroundAuthorityFactory,
) (ExportStyleDeploymentAuthority, error) {
	var zero ExportStyleDeploymentAuthority
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !validImportRefForSHA(pin) {
		return zero, v2input.ErrInvalid
	}
	raw, err := readExportPolicyFile(ctx, path, 64<<10)
	if err != nil {
		return zero, err
	}
	if rawSHA(raw) != pin {
		return zero, v2input.ErrInvalid
	}
	var wire exportStyleOperatorConfig
	if err := decodeImportDocument(raw, &wire); err != nil {
		return zero, err
	}
	if base.Other == nil {
		base.Other = ExportOriginalSchemaChildren{}
	}
	if base.Semantics == nil && base.SemanticsFactory == nil {
		base.SemanticsFactory = ExportCatalogDependencySemanticsFactory{}
	}
	if wire.Schema != "interchange-export-style-operator/v1" ||
		(base.Semantics == nil) == (base.SemanticsFactory == nil) || backgrounds == nil ||
		base.ICCSHA != wire.ICCSHA || base.ModelSHA != wire.ModelSHA {
		return zero, v2input.ErrInvalid
	}
	public, err := hex.DecodeString(wire.PersonOriginPublic)
	if err != nil || len(public) != ed25519.PublicKeySize ||
		hex.EncodeToString(public) != wire.PersonOriginPublic ||
		wire.PersonOriginIssuer == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	for _, sha := range []string{wire.ICCSHA, wire.ModelSHA, wire.StyleSHA,
		wire.BodySHA, wire.FitSHA, wire.DependencySHA, wire.RegistrationSHA,
		wire.MaskOriginSHA, wire.MaskArraySHA, wire.DetectorSHA} {
		if !validImportRefForSHA(sha) {
			return zero, v2input.ErrInvalid
		}
	}
	if len(wire.Models) > 32 {
		return zero, v2input.ErrInvalid
	}
	models := make(map[string]string, len(wire.Models))
	for name, sha := range wire.Models {
		if name == "" || !validImportRefForSHA(sha) {
			return zero, v2input.ErrInvalid
		}
		models[name] = sha
	}
	if len(wire.StyleWeights) > 32 {
		return zero, v2input.ErrInvalid
	}
	styleWeights := make(map[string]string, len(wire.StyleWeights))
	for name, weight := range wire.StyleWeights {
		if !exportModelIDs[name] || len(weight) < 1 || len(weight) > 240 ||
			base.ModelWeights == nil || base.ModelWeights[name] != weight {
			return zero, v2input.ErrInvalid
		}
		styleWeights[name] = weight
	}
	load := func(file exportOperatorPinnedFile) ([]byte, error) {
		if !filepath.IsAbs(file.Path) || filepath.Clean(file.Path) != file.Path ||
			!validImportRefForSHA(file.SHA) || file.Path == path {
			return nil, v2input.ErrInvalid
		}
		body, err := readExportPolicyFile(ctx, file.Path, 1<<20)
		if err != nil {
			return nil, err
		}
		if rawSHA(body) != file.SHA {
			return nil, v2input.ErrInvalid
		}
		return body, nil
	}
	personRaw, err := load(wire.Person)
	if err != nil {
		return zero, err
	}
	promptRaw, err := load(wire.Prompt)
	if err != nil {
		return zero, err
	}
	dependenciesRaw, err := load(wire.Dependencies)
	if err != nil {
		return zero, err
	}
	alignmentRaw, err := load(wire.Alignment)
	if err != nil {
		return zero, err
	}
	detectorRaw, err := load(wire.Detector)
	if err != nil {
		return zero, err
	}
	person, err := DecodeExportStatsPersonPolicyConfig(ctx, personRaw)
	if err != nil {
		return zero, err
	}
	prompt, err := DecodeExportPromptMaskPolicyConfig(ctx, promptRaw)
	if err != nil {
		return zero, err
	}
	dependencies, err := DecodeExportBodyDependencyPolicyConfig(ctx, dependenciesRaw)
	if err != nil {
		return zero, err
	}
	alignment, err := DecodeExportAlignmentPolicyConfig(ctx, alignmentRaw)
	if err != nil {
		return zero, err
	}
	detector, err := DecodeExportDetectorPolicyConfig(ctx, detectorRaw)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ExportStyleDeploymentAuthority{
		Base: base, BackgroundFactory: backgrounds, Models: models, StyleWeights: styleWeights,
		StyleRuntimeSHA: wire.StyleSHA,
		BodyRuntime: ExportBodyDeploymentRuntime{
			BodySHA: wire.BodySHA, FitSHA: wire.FitSHA,
			DependencySHA: wire.DependencySHA, RegistrationSHA: wire.RegistrationSHA,
			MaskOriginSHA: wire.MaskOriginSHA, MaskArraySHA: wire.MaskArraySHA,
			DetectorSHA: wire.DetectorSHA,
		},
		pinnedPolicies: &exportStylePinnedPolicies{
			Person: person, Prompt: prompt, Dependencies: append([]ExportBodyDependencyPolicyConfig(nil), dependencies...),
			Alignment: alignment, Detector: detector,
		},
		personOriginIssuer: wire.PersonOriginIssuer,
		personOriginPublic: append(ed25519.PublicKey(nil), public...),
	}, nil
}
