package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// Retains unique original producer roots independently of mutable proof slices.
func nativeQualifiedProducerSet(refs []v2input.Ref) (string, error) {
	if len(refs) > 64 {
		return "", v2input.ErrInvalid
	}
	owned := append([]v2input.Ref(nil), refs...)
	seen := map[uuid.UUID]bool{}
	for _, ref := range owned {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return "", v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return "", v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	sort.Slice(owned, func(i, j int) bool { return owned[i].ArtifactID.String() < owned[j].ArtifactID.String() })
	if owned == nil {
		owned = []v2input.Ref{}
	}
	body, err := canonical.Marshal(owned)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// Pairs preserve the detector associated with each landmark/parser root.
// A shared detector is permitted; duplicate pairs and conflicting digests are not.
func nativeQualifiedProducerPairs(pairs [][2]v2input.Ref) (string, error) {
	if len(pairs) > 64 {
		return "", v2input.ErrInvalid
	}
	owned := append([][2]v2input.Ref(nil), pairs...)
	seen := map[[2]v2input.Ref]bool{}
	digests := map[uuid.UUID]string{}
	for _, pair := range owned {
		if seen[pair] || pair[0].ArtifactID == pair[1].ArtifactID {
			return "", v2input.ErrInvalid
		}
		seen[pair] = true
		for _, ref := range pair {
			if ref.ArtifactID == uuid.Nil {
				return "", v2input.ErrInvalid
			}
			if _, err := domain.ParseDigest(ref.SHA256); err != nil {
				return "", v2input.ErrInvalid
			}
			if previous, ok := digests[ref.ArtifactID]; ok && previous != ref.SHA256 {
				return "", v2input.ErrInvalid
			}
			digests[ref.ArtifactID] = ref.SHA256
		}
	}
	sort.Slice(owned, func(i, j int) bool {
		if owned[i][0].ArtifactID != owned[j][0].ArtifactID {
			return owned[i][0].ArtifactID.String() < owned[j][0].ArtifactID.String()
		}
		return owned[i][1].ArtifactID.String() < owned[j][1].ArtifactID.String()
	})
	if owned == nil {
		owned = [][2]v2input.Ref{}
	}
	body, err := canonical.Marshal(owned)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
func nativeQualifiedProducerSets(faces []facePreparationProof, parsers []faceParsePreparationProof) (string, string, error) {
	faceRefs := make([][2]v2input.Ref, len(faces))
	parseRefs := make([][2]v2input.Ref, len(parsers))
	for i, proof := range faces {
		faceRefs[i] = [2]v2input.Ref{proof.objects[0][0].ref, proof.objects[1][0].ref}
	}
	for i, proof := range parsers {
		parseRefs[i] = [2]v2input.Ref{proof.evidence.objects[0][0].ref, proof.evidence.objects[1][0].ref}
	}
	faceSet, err := nativeQualifiedProducerPairs(faceRefs)
	if err != nil {
		return "", "", err
	}
	parserSet, err := nativeQualifiedProducerPairs(parseRefs)
	if err != nil {
		return "", "", err
	}
	return faceSet, parserSet, nil
}
