"""Keep independent replay owners alive through complete typed preparation."""
from .adoption import AdoptionInvalid, prepare_v2_adoption
from hashlib import sha256
from .render_recipe import decode_frozen_render_recipe
from .render_adoption_inputs import RenderAdoptionInputs
from .render_resources import validate_render_execution_profile
from .render_replay_reader import RenderReplayReader


def prepare_render_adoption(task_id, root, reader, inputs, replay, *,
                            scratch_root, max_bytes, max_tile_bytes):
    """Replay is an operator-configured qualified complete-provider context.

    It receives primary-scoped inputs and the verified frozen recipe, never the
    submitted root. Its owned read-only final pixels remain live until byte,
    change/protection and full staged-output closure checks finish. Provider
    installation/qualification and complete inventory resolution remain duties
    of this trusted replay dependency, not assertions supplied by the worker.
    """
    if not isinstance(inputs, RenderAdoptionInputs) or not callable(replay):
        raise AdoptionInvalid('RENDER_ADOPTION_REPLAY_REQUIRED')
    if (scratch_root is None or type(max_bytes) is not int or max_bytes<1
            or max_bytes>2**53-1 or type(max_tile_bytes) is not int or not 1<=max_tile_bytes<=2**53-1):
        raise AdoptionInvalid('RENDER_ADOPTION_SCRATCH_REQUIRED')
    evidence=reader.evidence(inputs.recipe)
    if (evidence.ref!=inputs.recipe or evidence.source!='input'
            or evidence.media_type!='application/json' or type(evidence.bytes) is not int
            or not 2<=evidence.bytes<=1048576):
        raise AdoptionInvalid('RENDER_ADOPTION_RECIPE_INVALID')
    raw=reader.read_json(inputs.recipe,1048576)
    if (not isinstance(raw,bytes) or len(raw)!=evidence.bytes
            or sha256(raw).hexdigest()!=inputs.recipe.sha256):
        raise AdoptionInvalid('RENDER_ADOPTION_RECIPE_INVALID')
    try:frozen=decode_frozen_render_recipe(raw)
    except ValueError as error:
        raise AdoptionInvalid('RENDER_ADOPTION_RECIPE_INVALID') from error
    from .render_qualification_pin import RenderQualificationPin
    if not isinstance(inputs.qualification_pin,RenderQualificationPin):
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_STALE')
    profile=inputs.execution_profile()
    pin=inputs.qualification_pin
    if (pin.model_lock_sha,pin.runtime_lock_sha,pin.backend)!=(profile.get('model_lock_sha'),profile.get('render_runtime_sha'),profile.get('backend')):
        raise AdoptionInvalid('RENDER_REPLAY_QUALIFICATION_MISMATCH')
    validate_render_execution_profile(profile,frozen,backend=profile.get('backend'),
        runtime_sha=profile.get('render_runtime_sha'),tile_size=profile.get('tile_size'),
        alpha_mode=profile.get('alpha_mode'))
    with replay(RenderReplayReader(reader), inputs, frozen) as expected:
        return prepare_v2_adoption(task_id=task_id,operation='render.compose',version='1',
            root=root,reader=reader,params_ref=inputs.recipe,render_inputs=inputs,
            render_expected=expected,layer_scratch_root=scratch_root,
            layer_scratch_bytes=max_bytes,render_max_tile_bytes=max_tile_bytes)
