package main

import (
	"context"
	"errors"
	"os"

	"photoeditor/backend/internal/interchange"
	"photoeditor/backend/internal/workflow"
)

// Keep the absent import installation truly absent: the scheduler rejects a
// present nil factory and must only start import processing with a full one.
func schedulerImportFactories(ctx context.Context, dependencies apiProcessDependencies) ([]workflow.SchedulerInterchangeImportFactory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	configPath, configSHA := os.Getenv("PHOTO_V30_IMPORT_OPERATOR_CONFIG"), os.Getenv("PHOTO_V30_IMPORT_OPERATOR_CONFIG_SHA256")
	if (configPath == "") != (configSHA == "") {
		return nil, errors.New("V30 scheduler import operator config requires file and SHA256")
	}
	if configPath != "" && dependencies.ImportOperatorFiles != nil {
		return nil, errors.New("conflicting V30 scheduler import operator declarations")
	}
	operatorDeclared := configPath != "" || dependencies.ImportOperatorFiles != nil || dependencies.ImportEntryBase != nil || dependencies.ImportPersonAcceptance != nil
	if !operatorDeclared {
		if dependencies.InterchangeImport == nil {
			return nil, nil
		}
		return []workflow.SchedulerInterchangeImportFactory{dependencies.InterchangeImport}, nil
	}
	if dependencies.InterchangeImport != nil || dependencies.ImportEntryBase == nil || dependencies.ImportPersonAcceptance == nil || (configPath == "" && dependencies.ImportOperatorFiles == nil) {
		return nil, errors.New("incomplete or conflicting V30 scheduler import installation")
	}
	files := dependencies.ImportOperatorFiles
	if configPath != "" {
		loaded, err := interchange.LoadImportSchedulerOperatorFiles(ctx, configPath, configSHA)
		if err != nil {
			return nil, err
		}
		files = &loaded
	}
	installed, err := files.Install(ctx, *dependencies.ImportEntryBase, dependencies.ImportPersonAcceptance)
	if err != nil {
		return nil, err
	}
	return []workflow.SchedulerInterchangeImportFactory{installed}, nil
}
