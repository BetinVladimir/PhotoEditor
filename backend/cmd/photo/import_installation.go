package main

import "photoeditor/backend/internal/workflow"

// Keep the absent import installation truly absent: the scheduler rejects a
// present nil factory and must only start import processing with a full one.
func schedulerImportFactories(dependencies apiProcessDependencies) []workflow.SchedulerInterchangeImportFactory {
	if dependencies.InterchangeImport == nil {
		return nil
	}
	return []workflow.SchedulerInterchangeImportFactory{dependencies.InterchangeImport}
}
