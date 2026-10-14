package main

import (
	"context"
	"errors"

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
		if base != nil || background != nil {
			return dependencies, errors.New("export operator capabilities require pinned operator declaration")
		}
		return dependencies, nil
	}
	if path == "" || pin == "" || base == nil || background == nil ||
		dependencies.InterchangeExportAuthority != nil || dependencies.InterchangeExport != nil {
		return dependencies, errors.New("export operator requires one pinned declaration and independent process semantics")
	}
	owned := *base
	owned.MaskModelLocks = append([]string(nil), base.MaskModelLocks...)
	authority, err := interchange.LoadExportStyleOperatorAuthority(ctx, path, pin, owned, background)
	if err != nil {
		return dependencies, err
	}
	dependencies.InterchangeExportAuthority = authority
	dependencies.InterchangeExportBase = nil
	dependencies.InterchangeBackgroundFactory = nil
	return dependencies, nil
}
