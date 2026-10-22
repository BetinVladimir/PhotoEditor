"""Effective-style match for immutable adopted native geometry parameters.

Canonical provenance is checked independently. A matching base Style ref does
not authorize arbitrary Apply job overrides, fitted body coefficients or scales.
"""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.body import BodyParams
from photo_worker.contracts.restoration import UpscaleParams
from photo_worker.contracts.composition import CropParams,CompositionParams,CompositionPlan


def validate_region_geometry_policy(effective,verified,*,model_lock_sha,composition_plans=None,composition_params=None):
    body=[];upscale=[];crop=[]
    for entry in verified:
        stage,raw,source,target=entry[:4]
        policy=entry[4] if len(entry)>4 else {}
        if stage.operation=='geometry.body':
            params=BodyParams.model_validate_json(canonical_bytes(raw));geometry=effective.geometry
            if geometry is None or not geometry.enabled or params.fit is not None or params.coefficients is None:
                raise ValueError('REGION_BODY_POLICY_MISMATCH')
            expected=dict(face_slim=geometry.face_slim,waist_slim=geometry.waist_slim,
                hip_slim=geometry.hip_slim,torso_slim=geometry.torso_slim,leg_length=geometry.leg_length)
            if (params.coefficients.model_dump(mode='json')!=expected
                    or params.strength!=effective.strength*geometry.body_strength
                    or params.max_displacement_image_fraction!=geometry.max_displacement_image_fraction
                    or params.algorithm!=geometry.algorithm or params.on_unsafe!=geometry.on_unsafe
                    or params.protect_background!=geometry.protect_background):
                raise ValueError('REGION_BODY_POLICY_MISMATCH')
            body.append((stage,params,source,target))
        elif stage.operation=='image.upscale':
            params=UpscaleParams.model_validate_json(canonical_bytes(raw));restoration=effective.extensions.restoration
            if restoration is None or restoration.upscale!=str(params.scale)+'x' or params.model_lock_id!=model_lock_sha:
                raise ValueError('REGION_UPSCALE_POLICY_MISMATCH')
            upscale.append((stage,params,source,target))
        elif stage.operation=='geometry.crop':
            params=CropParams.model_validate_json(canonical_bytes(raw));composition=effective.extensions.composition
            if composition_plans is None:composition_plans={} if not policy else {stage.plan.artifact_id:policy['plan']}
            if composition_params is None:composition_params={} if not policy else {policy['plan'].structure.artifact_id:policy['params']}
            if composition is None or not composition.enabled or not composition_plans or not composition_params:
                raise ValueError('REGION_COMPOSITION_POLICY_REQUIRED')
            plan=composition_plans.get(params.plan.artifact_id)
            if not isinstance(plan,CompositionPlan) or plan.image!=stage.source or plan.source_width!=source.canvas.width or plan.source_height!=source.canvas.height:
                raise ValueError('REGION_COMPOSITION_SOURCE_MISMATCH')
            configured=composition_params.get(plan.structure.artifact_id)
            if (not isinstance(configured,CompositionParams) or configured.structure!=plan.structure
                    or configured.aspect_width!=composition.aspect[0] or configured.aspect_height!=composition.aspect[1]
                    or configured.headroom_target!=composition.headroom
                    or configured.allow_rotation!=(composition.rotate_limit_deg>0)
                    or configured.max_rotation_degrees!=composition.rotate_limit_deg):
                raise ValueError('REGION_COMPOSITION_POLICY_MISMATCH')
            from photo_worker.operations.geometry.prepare import crop_geometry
            canvas,chain=crop_geometry(stage.source,source,params.plan,plan)
            if canvas!=target.canvas or chain!=target.transform_chain:
                raise ValueError('REGION_COMPOSITION_TRANSFORM_MISMATCH')
            crop.append((stage,params,source,target))
        elif stage.operation=='image.outpaint':
            from .outpaint import outpaint_policy
            params=outpaint_policy(effective,entry,model_lock_sha)
            crop.append((stage,params,source,target))
    requested_body=effective.geometry is not None and effective.geometry.enabled
    restoration=effective.extensions.restoration
    requested_upscale=restoration is not None and restoration.upscale!='off'
    requested_crop=effective.extensions.composition is not None and effective.extensions.composition.enabled
    if len(body)!=int(requested_body) or len(upscale)!=int(requested_upscale) or len(crop)!=int(requested_crop):
        raise ValueError('REGION_GEOMETRY_STAGE_COVERAGE_MISMATCH')
    return tuple(body),tuple(crop),tuple(upscale)
