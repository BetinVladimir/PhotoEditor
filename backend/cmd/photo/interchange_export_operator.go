package main

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/interchange"
)

// installInterchangeExportOperator binds operator-owned policy files to the
// process's independently installed schema/source/consent semantics. A file
// cannot manufacture those executable capabilities or a person-origin issuer.
func installInterchangeExportOperator(ctx context.Context, path, pin string,
	dependencies apiProcessDependencies) (apiProcessDependencies, error) {
	if err := ctx.Err(); err != nil {
		return dependencies, err
	}
	base := dependencies.InterchangeExportBase
	background := dependencies.InterchangeBackgroundFactory
	if path == "" && pin == "" {
		if base != nil || background != nil || dependencies.InterchangePersonOriginKey != nil ||
			dependencies.InterchangePersonIssuerID != uuid.Nil || dependencies.InterchangePersonProcess != nil {
			return dependencies, errors.New("export operator capabilities require pinned operator declaration")
		}
		return dependencies, nil
	}
	if path == "" || pin == "" || base == nil || background == nil ||
		dependencies.InterchangeExportAuthority != nil || dependencies.InterchangeExport != nil ||
		dependencies.InterchangePersonProcess != nil || dependencies.InterchangePersonOrigins != nil ||
		(dependencies.InterchangePersonOriginKey == nil) != (dependencies.InterchangePersonIssuerID == uuid.Nil) {
		return dependencies, errors.New("export operator requires one pinned declaration and independent process semantics")
	}
	owned := *base
	owned.MaskModelLocks = append([]string(nil), base.MaskModelLocks...)
	authority, err := interchange.LoadExportStyleOperatorAuthority(ctx, path, pin, owned, background)
	if err != nil {
		return dependencies, err
	}
	pinnedTrust := authority.PersonOriginTrust()
	if len(pinnedTrust) != 1 {
		return dependencies, errors.New("export operator person-origin trust unavailable")
	}
	if len(dependencies.InterchangePersonOriginTrust) != 0 {
		if len(dependencies.InterchangePersonOriginTrust) != len(pinnedTrust) {
			return dependencies, errors.New("export operator person-origin trust mismatch")
		}
		for key, public := range pinnedTrust {
			if !bytes.Equal(dependencies.InterchangePersonOriginTrust[key], public) {
				return dependencies, errors.New("export operator person-origin trust mismatch")
			}
		}
	}
	if dependencies.InterchangePersonOriginKey != nil {
		if dependencies.InterchangePersonOrigins != nil {
			return dependencies, errors.New("person-origin process signer conflicts with another issuer or trust map")
		}
		process, err := authority.InstallPersonOriginProcess(
			dependencies.InterchangePersonOriginKey, dependencies.InterchangePersonIssuerID)
		if err != nil {
			return dependencies, err
		}
		dependencies.InterchangePersonProcess = process
		dependencies.InterchangePersonOriginKey = nil
		dependencies.InterchangePersonIssuerID = uuid.Nil
	}
	dependencies.InterchangePersonOriginTrust = pinnedTrust
	dependencies.InterchangeExportAuthority = authority
	dependencies.InterchangeExportBase = nil
	dependencies.InterchangeBackgroundFactory = nil
	return dependencies, nil
}
