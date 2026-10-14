package main

import (
	"context"
	"errors"

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
	iccSHA, err := colorprofile.LoadWorkSHA(iccRoot)
	if err != nil {
		return dependencies, err
	}
	verifiedModelSHA, err := interchange.LoadExportModelLockSHA(ctx, modelPath, modelSHA)
	if err != nil {
		return dependencies, err
	}
	dependencies.InterchangeExportBase = &interchange.ExportDocumentValidatorInstallation{
		ICCSHA: iccSHA, ModelSHA: verifiedModelSHA,
	}
	dependencies.InterchangeBackgroundFactory = interchange.SQLExportStyleBackgroundFactory{}
	return dependencies, nil
}
