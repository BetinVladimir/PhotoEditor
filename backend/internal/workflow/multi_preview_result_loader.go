package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2render"
)

// LoadMultiPreviewResult binds verified manifest bytes and exact encoded byte
// SHA/length to a complete source read. It does not certify encoded format,
// metadata stripping or embedded ICC; final acceptance still owes those checks.
func LoadMultiPreviewResult(ctx context.Context, results MultiPreviewResults, objects ArtifactOpener, plan MultiPreviewPlan, content v2render.MultiResultContent, variant string) (RenderDerivativeResult, error) {
	var zero RenderDerivativeResult
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if objects == nil || (variant != "preview1600" && variant != "preview2560") {
		return zero, objectstore.ErrInvalid
	}
	if err := validateMultiPreviewPlan(plan); err != nil {
		return zero, err
	}
	rebuilt, err := CompileMultiPreviewPlan(plan.source, content, plan.srgb,plan.policy)
	if err != nil {
		return zero, err
	}
	index := 0
	if variant == "preview2560" {
		index = 1
	}
	expected := rebuilt.Documents()[index]
	image, err := content.Image()
	if err != nil {
		return zero, err
	}
	pair, err := results.Read(ctx, plan.source.Tenant, plan.run, variant)
	if err != nil {
		return zero, err
	}
	if len(pair) == 0 {
		return zero, objectstore.ErrMissing
	}
	result := RenderDerivativeResult{Task: pair[0].Task, Result: pair[0].Result, Attempt: pair[0].Attempt, Fence: pair[0].Fence}
	for _, output := range pair {
		if output.Params != string(expected.Params) {
			return zero, objectstore.ErrIntegrity
		}
		ref := review.ArtifactRef{ID: output.Artifact, SHA256: output.Object.SHA256}
		if output.Role == "derivative" {
			result.DerivativeRef = ref
			result.Derivative = output.Object
		} else {
			result.ManifestRef = ref
			result.ManifestObject = output.Object
		}
	}
	result.ManifestBytes, err = readRenderDerivativeJSON(ctx, objects, result.ManifestObject)
	if err != nil {
		return zero, err
	}
	result.Manifest, err = ParseRenderDerivativeManifest(result.ManifestBytes, result.ManifestRef, expected.Inputs[0], result.DerivativeRef, image, variant, plan.srgb)
	if err != nil {
		return zero, err
	}
	if result.Manifest.MediaType != result.Derivative.ContentType {
		return zero, objectstore.ErrIntegrity
	}
	if err = verifyMultiPreviewEncodedHash(ctx, objects, result.Derivative); err != nil {
		return zero, err
	}
	fresh, err := results.Read(ctx, plan.source.Tenant, plan.run, variant)
	if err != nil {
		return zero, err
	}
	if len(fresh) != len(pair) {
		return zero, objectstore.ErrIntegrity
	}
	for i := range pair {
		if pair[i] != fresh[i] {
			return zero, objectstore.ErrIntegrity
		}
	}
	return result, nil
}
func verifyMultiPreviewEncodedHash(ctx context.Context, objects ArtifactOpener, obj objectstore.Object) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if objects == nil || obj.Bytes < 1 || obj.Bytes > 54525952 || domainDigestInvalid(obj.SHA256) || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
		return objectstore.ErrInvalid
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return err
	}
	if reader == nil {
		return objectstore.ErrIntegrity
	}
	hash := sha256.New()
	buffer := make([]byte, 65536)
	var total int64
	for {
		if err = ctx.Err(); err != nil {
			break
		}
		var n int
		n, err = reader.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > obj.Bytes {
				err = objectstore.ErrIntegrity
				break
			}
			_, _ = hash.Write(buffer[:n])
		}
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}
		if n == 0 {
			err = io.ErrNoProgress
			break
		}
	}
	closeErr := reader.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if total != obj.Bytes || hex.EncodeToString(hash.Sum(nil)) != obj.SHA256 {
		return objectstore.ErrIntegrity
	}
	return nil
}
