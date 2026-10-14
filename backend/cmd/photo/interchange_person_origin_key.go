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
