package workflow

import "photoeditor/backend/internal/rawdevelopment"

// RAWDevelopment remains the workflow name for the shared frozen policy.
type RAWDevelopment = rawdevelopment.Policy

func validRAWDevelopment(p *RAWDevelopment) bool { return rawdevelopment.Valid(p) }
