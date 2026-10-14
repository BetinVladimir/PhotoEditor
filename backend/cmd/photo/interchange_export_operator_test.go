package main

import (
	"context"
	"strings"
	"testing"

	"photoeditor/backend/internal/interchange"
)

func TestExportOperatorBootstrapRejectsPartialProcessAuthority(t *testing.T) {
	base := &interchange.ExportDocumentValidatorInstallation{}
	background := interchange.SQLExportStyleBackgroundFactory{}
	for _, mode := range []string{"disabled", "base_only", "background_only", "path_only", "pin_only", "missing_base", "missing_background", "conflict", "direct_origin", "missing_file", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path, pin := "", ""
			deps := apiProcessDependencies{}
			switch mode {
			case "base_only":
				deps.InterchangeExportBase = base
			case "background_only":
				deps.InterchangeBackgroundFactory = background
			case "path_only":
				path = "/missing/export-operator.json"
			case "pin_only":
				pin = strings.Repeat("a", 64)
			default:
				if mode != "disabled" {
					path, pin = "/missing/export-operator.json", strings.Repeat("a", 64)
					deps.InterchangeExportBase = base
					deps.InterchangeBackgroundFactory = background
				}
			}
			switch mode {
			case "missing_base":
				deps.InterchangeExportBase = nil
			case "missing_background":
				deps.InterchangeBackgroundFactory = nil
			case "conflict":
				deps.InterchangeExportAuthority = &apiExportAuthority{}
			case "direct_origin":
				deps.InterchangePersonOrigins = &interchange.SQLExportPersonOriginCandidates{}
			case "cancelled":
				cancel()
			}
			installed, err := installInterchangeExportOperator(ctx, path, pin, deps)
			if mode == "disabled" {
				if err != nil || installed.InterchangeExportAuthority != nil {
					t.Fatal("disabled operator changed startup", err)
				}
				return
			}
			if err == nil || installed.InterchangeExportAuthority != deps.InterchangeExportAuthority {
				t.Fatal("incomplete operator accepted", err)
			}
			if mode == "cancelled" && err != context.Canceled {
				t.Fatal("cancelled operator proceeded", err)
			}
		})
	}
}
