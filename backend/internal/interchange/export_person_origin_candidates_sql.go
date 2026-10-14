package interchange

import (
	"context"
	"crypto/ed25519"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/semanticproposal"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// SQLExportPersonOriginCandidates discovers confirmed inline person masks from
// the frozen recipe graph. SQL resolves their original producer root; the
// issuer then repeats full current producer, physical and occurrence checks
// before signing. No request or archive may supply candidate identities.
type SQLExportPersonOriginCandidates struct {
	Objects   v2input.SessionAnalysisObjectReader
	Signer    PortablePersonOriginSigner
	IssuerID  uuid.UUID
	PublicKey ed25519.PublicKey
	Expected  v2qualification.Expected
}

func (s SQLExportPersonOriginCandidates) PrepareExportPersonOriginCandidates(ctx context.Context,
	intent CreateIntent, frozen FrozenExportDocuments, resolver ExportClosureNodeResolver,
	catalog SQLExportDocumentCatalog) ([]ExportPersonOriginPublished, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Objects == nil || s.Signer == nil || s.IssuerID == uuid.Nil ||
		len(s.PublicKey) != ed25519.PublicKeySize || resolver == nil || catalog.DB == nil ||
		catalog.Tenant == uuid.Nil || catalog.Who.UserID == uuid.Nil || catalog.Who.SessionID == uuid.Nil ||
		catalog.Request.Format != "photoeditor_bundle" || s.Expected.Operation != "mask.person" ||
		s.Expected.Version != "1" || !portablePersonOriginSHA.MatchString(s.Expected.ModelLockSHA) {
		return nil, v2input.ErrInvalid
	}
	_, compiled, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body())
	if err != nil {
		return nil, err
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil ||
		plan.Direction != "export" || plan.Format != "photoeditor_bundle" {
		return nil, v2input.ErrInvalid
	}
	occurrence, err := NewExportPersonOriginOccurrence(ctx, intent, frozen, resolver)
	if err != nil {
		return nil, err
	}
	graph := occurrence.reader
	personReader := semanticproposal.SQLTransferSourceReader{DB: catalog.DB, Objects: s.Objects,
		Bucket: catalog.Bucket, ProjectID: catalog.Request.ProjectID}
	candidates := make([]ExportPersonOriginPublished, 0)
	seen := map[string]bool{}
	for _, entry := range plan.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Binding == nil || entry.Recipe == nil {
			return nil, v2input.ErrInvalid
		}
		var recipe v2draft.FrozenRenderRecipe
		found := 0
		for _, root := range compiled.roots {
			if root.Contract != "FrozenRenderRecipe" {
				continue
			}
			node, err := readPersonOriginNode(ctx, graph, root)
			if err != nil {
				return nil, err
			}
			decoded, err := v2draft.DecodeFrozenRenderRecipe(node.Body)
			if err != nil {
				return nil, err
			}
			if decoded.Scalars.Source == *entry.Binding && decoded.RecipeSHA256 == entry.Recipe.SHA256 {
				recipe = decoded
				found++
			}
		}
		if found != 1 {
			return nil, v2input.ErrInvalid
		}
		var body struct {
			Overrides []v2input.Ref `json:"overrides"`
		}
		if json.Unmarshal(recipe.Recipe, &body) != nil {
			return nil, v2input.ErrInvalid
		}
		for _, override := range body.Overrides {
			node, err := readPersonOriginNode(ctx, graph, v2input.Entry{Ref: override, Contract: "Override"})
			if err != nil {
				return nil, err
			}
			var document struct {
				Masks []struct {
					Purpose   string          `json:"purpose"`
					Confirmed bool            `json:"confirmed"`
					Mask      json.RawMessage `json:"mask"`
				} `json:"mask_refs"`
			}
			if json.Unmarshal(node.Body, &document) != nil {
				return nil, v2input.ErrInvalid
			}
			for _, binding := range document.Masks {
				if binding.Purpose != "person_subject" || !binding.Confirmed {
					continue
				}
				mask, err := review.ParseMaskRef(binding.Mask)
				if err != nil || mask.Selector != "person" || mask.Protected || mask.ObjectID != nil ||
					mask.ModelLockID == nil || *mask.ModelLockID != s.Expected.ModelLockSHA {
					return nil, v2input.ErrInvalid
				}
				pixels := v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}
				root, err := catalog.DiscoverPortablePersonOriginRoot(ctx, *entry.Binding, pixels, rawSHA(binding.Mask))
				if err != nil {
					return nil, err
				}
				key := entry.ID.String() + ":" + root.ArtifactID.String()
				if seen[key] || len(candidates) >= 5000 {
					return nil, v2input.ErrInvalid
				}
				seen[key] = true
				origin, err := catalog.LookupPortablePersonOrigin(ctx, *entry.Binding, root, pixels)
				if err != nil {
					return nil, err
				}
				issuer, err := NewPortablePersonOriginIssuer(PortablePersonOriginIssuerConfig{
					Locator: catalog, Reader: personReader, Occurrence: occurrence, Signer: s.Signer,
					Who: catalog.Who, Tenant: catalog.Tenant, IssuerID: s.IssuerID,
					PublicKey: s.PublicKey, Report: origin.Report.Ref, Expected: s.Expected})
				if err != nil {
					return nil, err
				}
				scope := PortablePersonOriginExpectation{EntryID: entry.ID, Source: *entry.Binding,
					Recipe: *entry.Recipe, Override: override, MaskRef: root,
					MaskPixels: pixels, ModelLockSHA: s.Expected.ModelLockSHA}
				attachment, err := PreparePortablePersonOriginAttachment(ctx, issuer, scope)
				if err != nil {
					return nil, err
				}
				candidates = append(candidates, ExportPersonOriginPublished{Scope: scope, Attachment: attachment})
			}
		}
	}
	return candidates, ctx.Err()
}

var _ ExportPersonOriginCandidateFactory = SQLExportPersonOriginCandidates{}
