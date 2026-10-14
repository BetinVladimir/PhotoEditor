package main

import (
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"

	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/interchange"
)

// The process verifies the ICC registry and model lock independently before
// the pinned operator manifest may select its export policy files.
func installLocalInterchangeExportBase(ctx context.Context, operatorPath, operatorSHA,
	iccRoot, modelPath, modelSHA string, dependencies apiProcessDependencies,
) (apiProcessDependencies, error) {
	if err := ctx.Err(); err != nil {
		return dependencies, err
	}
	if operatorPath == "" && operatorSHA == "" {
		if modelPath != "" || modelSHA != "" {
			return dependencies, errors.New("export model lock requires pinned operator declaration")
		}
		return dependencies, nil
	}
	if dependencies.InterchangeExportBase != nil || dependencies.InterchangeBackgroundFactory != nil {
		if modelPath != "" || modelSHA != "" {
			return dependencies, errors.New("export base process declaration conflicts with injected authority")
		}
		return dependencies, nil
	}
	if operatorPath == "" || operatorSHA == "" || iccRoot == "" || modelPath == "" || modelSHA == "" {
		return dependencies, errors.New("export base requires ICC registry and pinned model lock")
	}
	validPinnedFile := func(path, pin string) bool {
		decoded, err := hex.DecodeString(pin)
		return filepath.IsAbs(path) && filepath.Clean(path) == path && err == nil &&
			len(decoded) == 32 && hex.EncodeToString(decoded) == pin
	}
	if !validPinnedFile(operatorPath, operatorSHA) || !validPinnedFile(modelPath, modelSHA) {
		return dependencies, errors.New("export base requires absolute files and lowercase SHA256 pins")
	}
	iccSHA, err := colorprofile.LoadWorkSHA(iccRoot)
	if err != nil {
		return dependencies, err
	}
	verifiedModelSHA, modelWeights, err := interchange.LoadExportModelLockWeights(ctx, modelPath, modelSHA)
	if err != nil {
		return dependencies, err
	}
	dependencies.InterchangeExportBase = &interchange.ExportDocumentValidatorInstallation{
		ICCSHA: iccSHA, ModelSHA: verifiedModelSHA, ModelWeights: modelWeights,
	}
	dependencies.InterchangeBackgroundFactory = interchange.SQLExportStyleBackgroundFactory{}
	return dependencies, nil
}
