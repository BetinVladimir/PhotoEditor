"""Full-face context content only; no photographic/runtime qualification proof."""
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.operations.semantic.preview_retouch_adapter_binding import bind_retouch_preview_adapter
from test_v27_exposure_geometry import geometry_fixture
from test_v27_retouch_preview_contract import retouch_params


def fixture(failure=None, *, geometry=None):
    edit,candidate_raw,intent_raw,_,mask_raw,*_=geometry if geometry is not None else geometry_fixture()
    params=retouch_params();intent=parse_json(intent_raw);intent.update(kind='retouch_region',ev=None)
    intent_raw=canonical_bytes(intent)
    params.update(source=edit['source'],candidate=edit['candidate'],snapshot_id=intent['target_snapshot_id'],
        intent={**edit['intent'],'sha256':sha256(intent_raw).hexdigest()})
    candidate=parse_json(candidate_raw);mask=parse_json(mask_raw)
    if failure=='candidate_noncanonical':
        candidate_raw+=b' '
        params['candidate']={**params['candidate'],'sha256':sha256(candidate_raw).hexdigest()}
    if failure=='intent_noncanonical':
        intent_raw+=b' '
        params['intent']={**params['intent'],'sha256':sha256(intent_raw).hexdigest()}
    context=dict(schema_version=1,algorithm='beautyfilter_v1',source=params['source'],candidate=params['candidate'],
        transferred_mask=candidate['mask'],protection=params['protection'],
        faces=[dict(landmarks=dict(artifact_id=str(uuid4()),sha256='f'*64),object_id=None,skin=None,manual=mask,manual_mode=True)],
        beauty=dict(enabled=True,algorithm='beautyfilter_v1',smoothing=.5,tone_evenness=.2,texture_keep=.8,
            skin_mask='face68_conservative_v1',protect_features=True),
        **{key:params[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha')})
    if failure=='source':context['source']=dict(context['source'],asset_version_id=str(uuid4()))
    if failure=='candidate':context['candidate']=dict(context['candidate'],artifact_id=str(uuid4()))
    if failure=='mask':context['transferred_mask']=dict(context['transferred_mask'],artifact_id=str(uuid4()))
    if failure=='pins':context['runtime_lock_sha']='0'*64
    if failure=='empty_faces':context['faces']=[]
    if failure=='duplicate_faces':context['faces']*=2
    if failure=='manual_mask':context['faces'][0]['manual']=dict(mask,confidence=.7)
    if failure=='landmark_alias':context['faces'][0]['landmarks']=params['plan']
    if failure=='algorithm':context['algorithm']='big_lama'
    if failure=='object_nil':context['faces'][0]['object_id']='00000000-0000-0000-0000-000000000000'
    if failure in ('skin_alias','skin_nil'):
        context['faces'][0]['skin']={**mask,'selector':'skin','artifact':
            params['plan'] if failure=='skin_alias' else dict(artifact_id='00000000-0000-0000-0000-000000000000',sha256='a'*64)}
    if failure=='landmark_contract':
        context['faces'].append({**context['faces'][0],'object_id':str(uuid4())})
    if failure in ('skin_landmarks','skin_manual_sha'):
        artifact=context['faces'][0]['landmarks'] if failure=='skin_landmarks' else {**mask['artifact'],'sha256':'0'*64}
        context['faces'][0]['skin']={**mask,'selector':'skin','artifact':artifact}
    if failure=='shared_landmark_sha':
        context['faces'][0]['object_id']=str(uuid4())
        context['faces'].append({**context['faces'][0],'object_id':str(uuid4()),
            'landmarks':{**context['faces'][0]['landmarks'],'sha256':'0'*64}})
    raw=canonical_bytes(context)
    context_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    if failure=='bytes':raw+=b' '
    return params,candidate_raw,intent_raw,context_ref,raw,mask_raw


def test_retouch_adapter_context_keeps_exact_transfer_mask_and_strength():
    params,candidate,intent,context=bind_retouch_preview_adapter(*fixture())
    assert context.source==params.source and context.transferred_mask==candidate.mask
    assert intent.kind=='retouch_region' and context.faces[0].manual_mode is True
    assert context.beauty.smoothing==.5 and context.beauty.texture_keep==.8


@pytest.mark.parametrize('failure',['source','candidate','mask','pins','empty_faces','duplicate_faces',
    'manual_mask','landmark_alias','algorithm','bytes','candidate_noncanonical','intent_noncanonical','object_nil','skin_alias','skin_nil','landmark_contract','skin_landmarks','skin_manual_sha','shared_landmark_sha'])
def test_retouch_adapter_context_refuses_changed_face_or_producer_binding(failure):
    with pytest.raises(ValueError):bind_retouch_preview_adapter(*fixture(failure))


def test_retouch_context_allows_identical_shared_collection_and_mask_artifacts():
    params, candidate, intent, ref, raw, mask_raw = fixture()
    context = parse_json(raw)
    manual = parse_json(mask_raw)
    first = {**context['faces'][0], 'object_id': str(uuid4()),
        'skin': {**manual, 'selector': 'skin'}}
    context['faces'] = [first, {**first, 'object_id': str(uuid4())}]
    raw = canonical_bytes(context)
    ref.sha256 = sha256(raw).hexdigest()
    owned, proposal, _, result = bind_retouch_preview_adapter(
        params, candidate, intent, ref, raw, mask_raw)
    assert len(result.faces) == 2
    assert result.faces[0].landmarks == result.faces[1].landmarks
    assert result.faces[0].skin.artifact == result.faces[0].manual.artifact
    assert result.candidate == owned.candidate and result.transferred_mask == proposal.mask
