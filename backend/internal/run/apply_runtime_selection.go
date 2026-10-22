package run

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
)

type applyRuntimeSelection struct {
 PersonRequired bool
 PersonBackend, PersonLock, RestorationBackend, RestorationLock, CreativeBackend, CreativeLock string
}
type applyRuntimeQuery interface { QueryRow(context.Context,string,...any) pgx.Row }
// Shared by read-only advisory discovery and fresh admission. Admission calls
// this only from its idempotency callback, never before durable receipt lookup.
func selectApplyRuntimes(ctx context.Context,tx applyRuntimeQuery,tid uuid.UUID,styleNeeds styleExecutionNeeds,identity bool)(out applyRuntimeSelection,err error){
 var e error
	seriesRequested := styleNeeds.Extensions.Series.Enabled
			geometryRequested := styleNeeds.Geometry.Enabled
			faceRequired := styleNeeds.Beauty.Enabled || styleNeeds.Defects.Enabled
			backgroundRequested := styleNeeds.Extensions.Background.Mode == "asset" || styleNeeds.Extensions.Background.Mode == "transparent" || styleNeeds.Extensions.Background.Mode == "solid" || styleNeeds.Extensions.Background.Mode == "blur" || styleNeeds.Extensions.Background.Mode == "generate"
			backgroundBlur := styleNeeds.Extensions.Background.Mode == "blur"
			personRequired := styleNeeds.Extensions.Culling.Enabled || styleNeeds.Extensions.Restoration.SensorDust == "conservative_auto" || applyRegionsNeedPerson(styleNeeds.Extensions.RegionAdjustments) || styleNeeds.Mode == "adaptive" || styleNeeds.Local.Enabled || seriesRequested || geometryRequested || faceRequired || backgroundRequested || styleNeeds.Extensions.Composition.Enabled
			personBackend, personRuntimeLock := "", ""
			if personRequired {
				if styleNeeds.Extensions.Culling.Enabled && !hasExactModel(styleNeeds.Models,"dinov2_vits14","facebook/dinov2-small") { return out,domain.NewError("STYLE_MODEL_BINDING_MISSING",409) }
                if !identity && !hasPersonModel(styleNeeds.Models) {
					return out, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				if !identity && geometryRequested && !hasExactModel(styleNeeds.Models, poseModelID, poseWeightID) {
					return out, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				if !identity && faceRequired && (!hasExactModel(styleNeeds.Models, faceDetectModelID, faceDetectWeightID) ||
					!hasExactModel(styleNeeds.Models, faceLandmarkModelID, faceLandmarkWeightID) ||
					!hasExactModel(styleNeeds.Models, faceParseModelID, faceParseWeightID)) {
					return out, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				if !identity && backgroundBlur && !hasExactModel(styleNeeds.Models, depthModelID, depthWeightID) {
					return out, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				e = tx.QueryRow(ctx, `
					SELECT w.lock_id, candidate.backend
					FROM photo.worker_registrations w
					CROSS JOIN LATERAL (
						SELECT backend
						FROM unnest(ARRAY['cuda','mps','cpu']) WITH ORDINALITY AS preferred(backend, priority)
						WHERE w.backends ? backend
						  AND (w.qualified_operations->'mask.person') ? backend
						  AND (NOT $2 OR (w.qualified_operations->'pose.detect') ? backend)
						  AND (NOT $3 OR ((w.qualified_operations->'face.detect') ? backend
						                    AND (w.qualified_operations->'face.landmarks') ? backend
						                    AND (w.qualified_operations->'face.parse') ? backend))
						  AND (NOT $4 OR (w.qualified_operations->'depth.predict') ? backend)
                          AND (NOT $5 OR (w.qualified_operations->'features.scene') ? backend)
						ORDER BY priority
						LIMIT 1
					) candidate
					WHERE w.tenant_id=$1
					  AND w.enabled
					  AND w.last_seen>clock_timestamp()-interval '30 seconds'
					  AND w.resource_classes ? 'gpu'
					  AND w.lock_id ~ '^[0-9a-f]{64}$'
					ORDER BY CASE candidate.backend WHEN 'cuda' THEN 0 WHEN 'mps' THEN 1 ELSE 2 END, w.worker_id
					LIMIT 1`, tid, geometryRequested, faceRequired, backgroundBlur, styleNeeds.Extensions.Culling.Enabled).Scan(&personRuntimeLock, &personBackend)
				if errors.Is(e, pgx.ErrNoRows) {
					return out, domain.NewError("PERSON_MASK_CAPABILITY_UNAVAILABLE", 409)
				}
				if e != nil {
					return out, e
				}
			}
			restorationDenoise := styleNeeds.Extensions.Restoration.Denoise == denoiseModelID
			restorationUpscale := styleNeeds.Extensions.Restoration.Upscale == "2x" || styleNeeds.Extensions.Restoration.Upscale == "4x"
			restorationBackend, restorationRuntimeLock := "", ""
			if restorationDenoise || restorationUpscale {
				if !identity && restorationDenoise && !hasExactModel(styleNeeds.Models, denoiseModelID, denoiseWeightID) {
					return out, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				upscaleID, upscaleWeight := upscale2ModelID, upscale2WeightID
				if styleNeeds.Extensions.Restoration.Upscale == "4x" {
					upscaleID, upscaleWeight = upscale4ModelID, upscale4WeightID
				}
				if !identity && restorationUpscale && !hasExactModel(styleNeeds.Models, upscaleID, upscaleWeight) {
					return out, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				e = tx.QueryRow(ctx, `SELECT w.lock_id,c.backend FROM photo.worker_registrations w CROSS JOIN LATERAL (
				 SELECT backend FROM unnest(ARRAY['cuda','mps','cpu']) WITH ORDINALITY p(backend,priority)
				 WHERE w.backends ? backend AND (NOT $2 OR (w.qualified_operations->'image.denoise') ? backend)
				  AND (NOT $3 OR (w.qualified_operations->'image.upscale') ? backend) ORDER BY priority LIMIT 1) c
				 WHERE w.tenant_id=$1 AND w.enabled AND w.last_seen>clock_timestamp()-interval '30 seconds' AND w.resource_classes ? 'gpu'
				  AND w.lock_id ~ '^[0-9a-f]{64}$' ORDER BY CASE c.backend WHEN 'cuda' THEN 0 WHEN 'mps' THEN 1 ELSE 2 END,w.worker_id LIMIT 1`, tid, restorationDenoise, restorationUpscale).Scan(&restorationRuntimeLock, &restorationBackend)
				if errors.Is(e, pgx.ErrNoRows) {
					return out, domain.NewError("RESTORATION_CAPABILITY_UNAVAILABLE", 409)
				}
				if e != nil {
					return out, e
				}
			}
			creativeInpaint := styleNeeds.Extensions.Inpainting.Enabled
			creativeGeneration := styleNeeds.Extensions.Generation.Enabled || styleNeeds.Extensions.Background.Mode == "generate" || styleNeeds.Extensions.Composition.Enabled && styleNeeds.Extensions.Composition.OutpaintPolicy == "review"
			creativeBackend, creativeRuntimeLock := "", ""
			if creativeInpaint || creativeGeneration {
				if !identity && (creativeInpaint && !hasExactModel(styleNeeds.Models, inpaintModelID, inpaintWeightID) || creativeGeneration && !hasExactModel(styleNeeds.Models, generationModelID, generationWeightID)) {
					return out, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				e = tx.QueryRow(ctx, `SELECT w.lock_id,c.backend FROM photo.worker_registrations w CROSS JOIN LATERAL (
				 SELECT backend FROM unnest(ARRAY['cuda','mps','cpu']) WITH ORDINALITY p(backend,priority)
					 WHERE w.backends ? backend AND (NOT $2 OR (w.qualified_operations->'image.inpaint') ? backend)
				  AND (NOT $3 OR ((w.qualified_operations->'image.generate') ? backend OR (w.qualified_operations->'image.outpaint') ? backend))
				  AND (NOT $4 OR (w.qualified_operations->'image.outpaint') ? backend) ORDER BY priority LIMIT 1) c
				 WHERE w.tenant_id=$1 AND w.enabled AND w.last_seen>clock_timestamp()-interval '30 seconds' AND w.resource_classes ? 'gpu'
				  AND w.lock_id ~ '^[0-9a-f]{64}$' ORDER BY CASE c.backend WHEN 'cuda' THEN 0 WHEN 'mps' THEN 1 ELSE 2 END,w.worker_id LIMIT 1`, tid, creativeInpaint, styleNeeds.Extensions.Generation.Enabled || styleNeeds.Extensions.Background.Mode == "generate", styleNeeds.Extensions.Composition.Enabled && styleNeeds.Extensions.Composition.OutpaintPolicy == "review").Scan(&creativeRuntimeLock, &creativeBackend)
				if errors.Is(e, pgx.ErrNoRows) {
					return out, domain.NewError("CREATIVE_CAPABILITY_UNAVAILABLE", 409)
				}
				if e != nil {
					return out, e
				}
			}

 return applyRuntimeSelection{personRequired,personBackend,personRuntimeLock,restorationBackend,restorationRuntimeLock,creativeBackend,creativeRuntimeLock},nil
}
