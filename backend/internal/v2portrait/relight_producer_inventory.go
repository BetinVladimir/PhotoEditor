package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// RelightProducerDocuments contains original producer bytes discovered by the
// server. Compilation validates content only; live SQL authority and manual
// pixel replay are separate mandatory admission checks.
type RelightProducerDocuments struct {
	Depth  RelightDepthProducer
	Person *RelightPersonProducer
	Manual []RelightManualProducer
}

func mergeRelightInputs(groups ...[]v2input.Entry) ([]v2input.Entry, error) {
	seen := map[uuid.UUID]v2input.Entry{}
	for _, group := range groups {
		for _, entry := range group {
			if entry.Ref.ArtifactID == uuid.Nil {
				return nil, v2input.ErrInvalid
			}
			if old, ok := seen[entry.Ref.ArtifactID]; ok && old != entry {
				return nil, v2input.ErrInvalid
			}
			seen[entry.Ref.ArtifactID] = entry
		}
	}
	if len(seen) > 512 {
		return nil, v2input.ErrInvalid
	}
	entries := make([]v2input.Entry, 0, len(seen))
	for _, entry := range seen {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	return entries, nil
}

func CompileRelightProducerInputs(ctx context.Context, params RelightParams, pin RelightQualificationPin, documents RelightInputDocuments, producers RelightProducerDocuments) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := RelightMaskInputs(params, pin, documents.Source, documents.Subject, documents.Protected); err != nil {
		return nil, err
	}
	if err := VerifyRelightDepthProducerParams(params, producers.Depth.Ref, producers.Depth.Body); err != nil {
		return nil, err
	}
	entries := []v2input.Entry{{Ref: producers.Depth.Ref, Contract: "Params"}}
	subject, err := review.ParseMaskRef(documents.Subject)
	if err != nil {
		return nil, err
	}
	if subject.Selector == "person" {
		if producers.Person == nil {
			return nil, v2input.ErrInvalid
		}
		if err = VerifyRelightPersonProducerParams(params, producers.Person.Ref, producers.Person.Body); err != nil {
			return nil, err
		}
		entries = append(entries, v2input.Entry{Ref: producers.Person.Ref, Contract: "Params"})
	} else if subject.Selector != "manual" || producers.Person != nil {
		return nil, v2input.ErrInvalid
	}
	if len(producers.Manual) < 1 || len(producers.Manual) > 2 {
		return nil, v2input.ErrInvalid
	}
	roots := make([]review.ManualRasterDocument, len(producers.Manual))
	seen := map[uuid.UUID]bool{}
	for i, producer := range producers.Manual {
		if seen[producer.Root.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		seen[producer.Root.ArtifactID] = true
		roots[i], err = review.ParseManualRasterDocument(producer.RootBody, review.ArtifactRef{ID: producer.Root.ArtifactID, SHA256: producer.Root.SHA256})
		if err != nil {
			return nil, err
		}
	}
	used := map[int]bool{}
	for _, protected := range []bool{false, true} {
		if !protected && subject.Selector == "person" {
			continue
		}
		selected := params.Subject
		raw := documents.Subject
		if protected {
			selected = params.Protected
			raw = documents.Protected
		}
		matched := -1
		for i, root := range roots {
			mask := root.Target
			if protected {
				mask = root.Protected
			}
			sha, e := canonical.Hash(mask)
			if e != nil {
				return nil, e
			}
			if sha == selected.SHA256 {
				if matched != -1 {
					return nil, v2input.ErrInvalid
				}
				matched = i
			}
		}
		if matched < 0 {
			return nil, v2input.ErrInvalid
		}
		producer := producers.Manual[matched]
		root := roots[matched]
		if _, err = ResolveRelightManualPlan(ctx, params, documents.Source, raw, producer.ParamsBody, producer.RootBody, producer.VectorsBody, producer.Original, producer.Root, protected); err != nil {
			return nil, err
		}
		if used[matched] {
			continue
		}
		used[matched] = true
		entries = append(entries, v2input.Entry{Ref: producer.Root, Contract: "RasterizedMask"}, v2input.Entry{Ref: producer.Original, Contract: "Params"},
			v2input.Entry{Ref: v2input.Ref{ArtifactID: root.Target.Artifact.ID, SHA256: root.Target.Artifact.SHA256}, Contract: "NpyArray"},
			v2input.Entry{Ref: v2input.Ref{ArtifactID: root.Protected.Artifact.ID, SHA256: root.Protected.Artifact.SHA256}, Contract: "NpyArray"})
		vectors := v2input.Ref{ArtifactID: root.Vectors.ID, SHA256: root.Vectors.SHA256}
		if vectors != producer.Original {
			entries = append(entries, v2input.Entry{Ref: vectors, Contract: "ManualMask"})
		}
	}
	if len(used) != len(producers.Manual) {
		return nil, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return mergeRelightInputs(entries)
}

// CompileRelightFullInputPlan adds all original producer dependencies to direct
// operation inputs. Returned inventory is content-validated, not SQL-authorized.
func CompileRelightFullInputPlan(ctx context.Context, binding v2input.SessionVersionBinding, documents RelightInputDocuments, producers RelightProducerDocuments, paramsRaw []byte, runtimeSHA string, capacity LocalResourceEstimate, pin RelightQualificationPin) (RelightInputPlan, error) {
	var zero RelightInputPlan
	plan, err := CompileRelightInputPlan(ctx, binding, documents, paramsRaw, runtimeSHA, capacity, pin)
	if err != nil {
		return zero, err
	}
	var params RelightParams
	if json.Unmarshal(plan.Plan.Params, &params) != nil {
		return zero, v2input.ErrInvalid
	}
	additional, err := CompileRelightProducerInputs(ctx, params, pin, documents, producers)
	if err != nil {
		return zero, err
	}
	plan.Inputs, err = mergeRelightInputs(plan.Inputs, additional)
	if err != nil {
		return zero, err
	}
	return plan, ctx.Err()
}
