package v2input

// AuthoredReplayChild is an explicit typed readable edge. Opaque observations
// below are retained independently; they cannot downgrade or invent this type.
type AuthoredReplayChild struct {
	Ref      Ref
	Contract string
}
type AuthoredReplayChildren struct {
	Typed  []AuthoredReplayChild
	Opaque []Ref
}

// AuthoredNativeReplayRequestChildren validates the exact canonical request
// before projecting it. The scheduler must resolve all edges under actual SQL
// producer scope, then rerun this traversal after physical request publication.
// Neither this projection nor a ready artifact is producer authorization.
func AuthoredNativeReplayRequestChildren(raw []byte) (AuthoredReplayChildren, error) {
	request, err := DecodeAuthoredNativeReplayRequest(raw)
	if err != nil {
		return AuthoredReplayChildren{}, err
	}
	result := AuthoredReplayChildren{}
	add := func(ref Ref, contract string) {
		result.Typed = append(result.Typed, AuthoredReplayChild{Ref: ref, Contract: contract})
	}
	add(request.Consumer.Params, "Params")
	add(request.Consumer.Source.Source, "WorkingImage")
	add(request.GeometryParams, "Params")
	if request.PhaseReceipt != nil {
		add(*request.PhaseReceipt, "AuthoredCanonicalPhaseReceipt")
	}
	add(request.Original.Task.Params, "Params")
	// Actual image.upscale/1 root is the WorkingImage, not an ImageResult.
	add(request.Original.Root, "WorkingImage")
	add(request.Original.Image, "WorkingImage")
	add(request.Original.Report, "UpscaleReport")
	result.Opaque = append(result.Opaque, request.Original.Task.Outputs...)
	add(request.NativeSource.Source, "WorkingImage")
	seen := map[[16]byte]Ref{}
	kinds := map[[16]byte]string{}
	for _, edge := range result.Typed {
		key := [16]byte(edge.Ref.ArtifactID)
		if prior, ok := seen[key]; ok && prior != edge.Ref {
			return AuthoredReplayChildren{}, ErrInvalid
		}
		if prior, ok := kinds[key]; ok && prior != edge.Contract {
			return AuthoredReplayChildren{}, ErrInvalid
		}
		seen[key] = edge.Ref
		kinds[key] = edge.Contract
	}
	for _, ref := range result.Opaque {
		key := [16]byte(ref.ArtifactID)
		if prior, ok := seen[key]; ok && prior != ref {
			return AuthoredReplayChildren{}, ErrInvalid
		}
		seen[key] = ref
	}
	return result, nil
}
