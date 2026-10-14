package main

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/interchange"
)

func installLocalInterchangePersonKey(ctx context.Context, path, publicSHA, issuer string,
	dependencies apiProcessDependencies) (apiProcessDependencies, error) {
	if err := ctx.Err(); err != nil {
		return dependencies, err
	}
	if path == "" && publicSHA == "" && issuer == "" {
		return dependencies, nil
	}
	if path == "" || publicSHA == "" || issuer == "" ||
		dependencies.InterchangePersonOriginKey != nil || dependencies.InterchangePersonIssuerID != uuid.Nil ||
		dependencies.InterchangePersonProcess != nil {
		return dependencies, errors.New("person-origin signing key requires one complete process declaration")
	}
	id, err := uuid.Parse(issuer)
	if err != nil || id == uuid.Nil {
		return dependencies, errors.New("person-origin issuer ID invalid")
	}
	key, err := interchange.LoadProcessPersonOriginKey(ctx, path, publicSHA)
	if err != nil {
		return dependencies, err
	}
	dependencies.InterchangePersonOriginKey = key
	dependencies.InterchangePersonIssuerID = id
	return dependencies, nil
}

// The scheduler may verify person-origin signatures using the pinned public
// manifest, but must never receive the API's signing secret declaration.
func installRoleInterchangePersonKey(ctx context.Context, role, path, publicSHA, issuer string,
	dependencies apiProcessDependencies) (apiProcessDependencies, error) {
	if err := ctx.Err(); err != nil {
		return dependencies, err
	}
	if role == "api" {
		return installLocalInterchangePersonKey(ctx, path, publicSHA, issuer, dependencies)
	}
	if path != "" || publicSHA != "" || issuer != "" || dependencies.InterchangePersonOriginKey != nil ||
		dependencies.InterchangePersonIssuerID != uuid.Nil || dependencies.InterchangePersonProcess != nil {
		return dependencies, errors.New("person-origin private signer is restricted to API process")
	}
	return dependencies, nil
}
