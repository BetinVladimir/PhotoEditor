"""Independent retouch adoption linkage with synthetic metadata authority."""
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.v2.adoption import ArtifactRef,ArtifactEvidence,AdoptionInvalid
from photo_worker.v2.retouch_preview_adoption import verify_retouch_preview_output
from test_v27_retouch_preview_result_binding import fixture


@pytest.mark.parametrize('failure',[None,'denied','integer_true','foreign_root','wrong_role','late_denied','foreign_params'])
def test_retouch_adoption_requires_exact_live_bound_output(failure):
    params,root,display_raw,proof,proof_raw,image,image_raw,source_raw=fixture()
    convert=lambda value:ArtifactRef(value.artifact_id,value.sha256)
    root,proof,image=map(convert,(root,proof,image))
    params_raw=canonical_bytes(params);params_ref=ArtifactRef(uuid4(),sha256(params_raw).hexdigest())
    def ref(value):return ArtifactRef.parse(value)
    docs={params_ref:params_raw,root:display_raw,proof:proof_raw,image:image_raw,ref(params['source']['source']):source_raw}
    rows={value:ArtifactEvidence(value,'application/json',len(raw),
        'preview_presentation' if value==root else 'preview_evidence' if value==proof else 'working_image' if value==image else None,
        'output' if value in (root,proof,image) else 'input') for value,raw in docs.items()}
    display=parse_json(display_raw);working=parse_json(image_raw)
    for role,value in [('pixels',working['pixels']),*[(role,display[role]) for role in ('before','after','affected','protected')]]:
        value=ref(value);rows[value]=ArtifactEvidence(value,'application/x-npy' if role=='pixels' else 'image/png',100,role,'output')
    if failure=='foreign_root':rows[root]=ArtifactEvidence(root,'application/json',len(display_raw),'preview_presentation','input')
    if failure=='wrong_role':rows[proof]=ArtifactEvidence(proof,'application/json',len(proof_raw),'working_image','output')
    if failure=='foreign_params':
        params_raw=canonical_bytes(dict(params,target_revision='7'));params_ref=ArtifactRef(uuid4(),sha256(params_raw).hexdigest())
        docs[params_ref]=params_raw;rows[params_ref]=ArtifactEvidence(params_ref,'application/json',len(params_raw),None,'input')
    reads=[]
    class Reader:
        def evidence(self,value):return rows[value]
        def read_json(self,value,maximum):reads.append(value);return docs[value]
    def admission(raw):
        assert raw==params_raw
        if failure=='integer_true':return 1
        return failure!='denied' and not (failure=='late_denied' and len(reads)>1)
    def run():return verify_retouch_preview_output(Reader(),params_ref,root,
        verify_admission=admission,check_active=lambda:None)
    if failure:
        with pytest.raises(ValueError):run()
        if failure in ('denied','integer_true'):assert reads==[params_ref]
    else:
        result=run();assert result[0].kind=='retouch_region' and len(reads)==5


def test_retouch_adoption_cannot_use_generic_schema_validation_without_live_context():
    from photo_worker.v2.adoption import prepare_v2_adoption
    with pytest.raises(AdoptionInvalid,match='ADOPTION_CONTEXT_REQUIRED'):
        prepare_v2_adoption(task_id=uuid4(),operation='semantic.preview_retouch_region',version='1',
            root=ArtifactRef(uuid4(),'a'*64),reader=object())
