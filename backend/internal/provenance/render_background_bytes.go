package provenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Qualify physical background content without treating it as a WorkingImage or
// granting execution. Every occurrence retains its own historical catalog scope.
func verifySigningRenderBackgroundBytes(ctx context.Context, catalog v2draft.ArtifactCatalog, objects SigningObjects, who auth.Identity, ready ReadySigningExport, evidence SigningRenderClosureEvidence, workICC string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pins := map[uuid.UUID]v2input.Ref{}
	seen := map[uuid.UUID]bool{}
	hashed := map[uuid.UUID]objectstore.Object{}
	for _, entry := range evidence.inventory.Entries {
		if entry.Contract != "StyleBackgroundAsset" {
			continue
		}
		if !ref(entry.Ref) {
			return ErrInvalid
		}
		if _, exists := pins[entry.Ref.ArtifactID]; exists {
			return ErrInvalid
		}
		pins[entry.Ref.ArtifactID] = entry.Ref
	}
	for _, occurrence := range evidence.inventory.Occurrences {
		node := occurrence.Node
		if node.Contract != "StyleBackgroundAsset" {
			continue
		}
		if catalog == nil || objects == nil || pins[node.Ref.ArtifactID] != node.Ref || node.ExpectedBackground == nil {
			return ErrInvalid
		}
		// Own optional AssumeProfile before external catalogue I/O.
		raw, err := canonical.Marshal(node.ExpectedBackground)
		if err != nil {
			return ErrInvalid
		}
		pin, err := v2draft.DecodeFrozenBackgroundAsset(raw)
		if err != nil || pin.Source != node.Ref || pin.WorkICCSHA != workICC {
			return ErrInvalid
		}
		lookup := func() (objectstore.Object, error) {
			isolated := node
			owned := pin
			if pin.AssumeProfile != nil {
				value := *pin.AssumeProfile
				owned.AssumeProfile = &value
			}
			isolated.ExpectedBackground = &owned
			return catalog.Lookup(ctx, who, ready.Tenant, isolated)
		}
		object, err := lookup()
		if err != nil {
			return err
		}
		if object != occurrence.Object || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.SHA256 != pin.Source.SHA256 || object.Bytes != pin.ExpectedBytes || object.ContentType != pin.MediaType {
			return ErrInvalid
		}
		if prior, exists := hashed[node.Ref.ArtifactID]; exists {
			if prior != object {
				return ErrInvalid
			}
		} else {
			if err := hashSigningRenderBackground(ctx, objects, object); err != nil {
				return err
			}
			hashed[node.Ref.ArtifactID] = object
		}
		current, err := lookup()
		if err != nil {
			return err
		}
		if current != object {
			return ErrInvalid
		}
		seen[node.Ref.ArtifactID] = true
	}
	if len(seen) != len(pins) {
		return ErrInvalid
	}
	return ctx.Err()
}

func hashSigningRenderBackground(ctx context.Context, objects SigningObjects, object objectstore.Object) error {
	return hashSigningRenderObject(ctx, objects, object, 512<<20)
}

func hashSigningRenderObject(ctx context.Context, objects SigningObjects, object objectstore.Object, maxBytes int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if objects == nil || object.Bytes < 1 || maxBytes < 1 || maxBytes > 9007199254740991 || object.Bytes > maxBytes || !digest(object.SHA256) {
		return ErrInvalid
	}
	cache, key, hit := signingRenderCachedObject(ctx, objects, object)
	if hit {
		return ctx.Err()
	}
	stream, err := objects.Open(ctx, object)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return err
	}
	if stream == nil {
		return ErrInvalid
	}
	hash := sha256.New()
	size, readErr := io.Copy(hash, io.LimitReader(signingContextReader{ctx: ctx, reader: stream}, object.Bytes+1))
	closeErr := stream.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if size != object.Bytes || hex.EncodeToString(hash.Sum(nil)) != object.SHA256 {
		return ErrInvalid
	}
	cache.remember(key)
	return nil
}
