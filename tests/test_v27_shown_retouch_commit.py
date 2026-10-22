"""Exact pinned document handoff to dedicated SQL commit; no SQL execution."""
from hashlib import sha256
from uuid import UUID, uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.v2.adoption import AdoptionInvalid, ArtifactEvidence, ArtifactRef
from photo_worker.v2.shown_retouch_commit import shown_retouch_commit_inputs
from photo_worker.v2.shown_retouch_installation import ShownRetouchAdoptionInstallation
from test_v27_retouch_preview_adopt_execute import copy_documents_fixture


@pytest.mark.parametrize('mode',[None,'changed_params','changed_output','changed_shown','revoked_read','final_admission','admission_lost_params','admission_lost_output','admission_lost_shown'])
def test_retouch_commit_handoff_rechecks_every_raw_document(tmp_path,mode):
    params,documents,path = copy_documents_fixture(tmp_path)
    from uuid import uuid5
    from photo_worker.contracts.semantic_preview import ShownRetouchAdoptionInput
    body = parse_json(canonical_bytes(params))
    body['preview_task_id'] = str(uuid5(params.preview_run_id, 'semantic.preview_retouch_region:' + str(params.target_id)))
    params = ShownRetouchAdoptionInput.model_validate_json(canonical_bytes(body))
    params_raw = canonical_bytes(params)
    shown_raw = documents[str(params.working_image.artifact_id)]
    output = parse_json(shown_raw)
    output['pixels'] = dict(artifact_id=str(uuid4()),sha256=params.pixels.sha256)
    output_raw = canonical_bytes(output)
    params_ref = ArtifactRef(uuid4(),sha256(params_raw).hexdigest())
    root = ArtifactRef(uuid4(),sha256(output_raw).hexdigest())
    shown_ref = ArtifactRef(params.working_image.artifact_id,params.working_image.sha256)
    original = ArtifactRef(params.pixels.artifact_id,params.pixels.sha256)
    copied = ArtifactRef(UUID(output['pixels']['artifact_id']),params.pixels.sha256)
    bodies = {params_ref:params_raw,root:output_raw,shown_ref:shown_raw}
    evidence = {ref:ArtifactEvidence(ref,'application/json',len(raw),
        'working_image' if ref==root else None,'output' if ref==root else 'input')
        for ref,raw in bodies.items()}
    evidence[original] = ArtifactEvidence(original,'application/x-npy',path.stat().st_size,None,'input')
    evidence[copied] = ArtifactEvidence(copied,'application/x-npy',path.stat().st_size,'pixels','output')
    active = True
    admissions = 0
    admitted = True
    reads = []
    counts = {}
    class Reader:
        def evidence(self,ref):
            assert active
            return evidence[ref]
        def read_json(self,ref,max_bytes):
            nonlocal active, admitted
            assert active and max_bytes==1048576
            counts[ref] = counts.get(ref,0)+1
            reads.append(ref)
            raw = bodies[ref]
            if counts[ref]==2:
                revoked = dict(admission_lost_params=params_ref,admission_lost_output=root,admission_lost_shown=shown_ref)
                if revoked.get(mode)==ref:
                    admitted = False
                if mode=='revoked_read' and ref==params_ref:
                    active = False
                changed = dict(changed_params=params_ref,changed_output=root,changed_shown=shown_ref)
                if changed.get(mode)==ref:
                    raw += b' '
            return raw
    def admission(raw):
        nonlocal admissions
        assert active and raw==params_raw
        admissions += 1
        return admitted and (mode!='final_admission' or len(reads)<6)
    installation = ShownRetouchAdoptionInstallation(params_ref,admission)
    def run():
        return shown_retouch_commit_inputs(Reader(),installation,root,check_active=lambda:active)
    if mode is None:
        assert run()==(params_ref.artifact_id,params_ref.sha256,params_raw.decode(),
            root.artifact_id,root.sha256,output_raw.decode(),shown_raw.decode())
        assert admissions>5
        assert reads==[params_ref,shown_ref,root,params_ref,root,shown_ref]
    else:
        with pytest.raises(AdoptionInvalid):
            run()
        if mode=='revoked_read':
            assert reads==[params_ref,shown_ref,root,params_ref]
        if mode=='final_admission':
            assert admissions>5
        stopped = dict(admission_lost_params=4,admission_lost_output=5,admission_lost_shown=6)
        if mode in stopped:
            assert len(reads)==stopped[mode]


def test_retouch_commit_requires_typed_installation():
    with pytest.raises(AdoptionInvalid,match='INSTALLATION_REQUIRED'):
        shown_retouch_commit_inputs(None,None,None,check_active=lambda:True)
