"""Own retouch copy contract and live evidence boundaries; no inference."""
from dataclasses import replace
from hashlib import sha256
from uuid import uuid4, uuid5

import pytest

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.semantic_preview import ShownRetouchAdoptionInput
from photo_worker.v2.adoption import AdoptionInvalid, ArtifactEvidence, ArtifactRef
from photo_worker.v2.shown_retouch_adoption import verify_shown_retouch_output
from test_v27_background_cleanup_preview_adopt_execute import copy_documents_fixture


@pytest.mark.parametrize('mode', [None, 'foreign_params', 'metadata', 'pixels_sha',
    'root_alias', 'role', 'source', 'size', 'raw_sha', 'revoked', 'authority_lost'])
def test_retouch_copy_output_binding(tmp_path, mode):
    # Reuse only the physical WorkingImage/NPY fixture. Own Params must carry
    # the retouch producer UUID; background-cleanup producer authority is invalid.
    old, documents, path = copy_documents_fixture(tmp_path)
    body = parse_json(canonical_bytes(old))
    body['preview_task_id'] = str(uuid5(old.preview_run_id,
        'semantic.preview_retouch_region:' + str(old.target_id)))
    params = ShownRetouchAdoptionInput.model_validate_json(canonical_bytes(body))
    params_raw = canonical_bytes(old if mode == 'foreign_params' else params)
    params_ref = ArtifactRef(uuid4(), sha256(params_raw).hexdigest())
    shown_ref = ArtifactRef(params.working_image.artifact_id, params.working_image.sha256)
    shown_raw = documents[str(shown_ref.artifact_id)]
    output = parse_json(shown_raw)
    output['pixels'] = dict(artifact_id=str(uuid4()), sha256=params.pixels.sha256)
    if mode == 'metadata': output['color']['icc_sha256'] = '0' * 64
    if mode == 'pixels_sha': output['pixels']['sha256'] = '0' * 64
    output_raw = canonical_bytes(output)
    root = ArtifactRef(shown_ref.artifact_id if mode == 'root_alias' else uuid4(),
        sha256(output_raw).hexdigest())
    from uuid import UUID
    copied = ArtifactRef(UUID(output['pixels']['artifact_id']), output['pixels']['sha256'])
    original = ArtifactRef(params.pixels.artifact_id, params.pixels.sha256)
    bodies = {params_ref: params_raw, shown_ref: shown_raw, root: output_raw}
    evidence = {ref: ArtifactEvidence(ref, 'application/json', len(raw),
        'working_image' if ref == root else None, 'output' if ref == root else 'input')
        for ref, raw in bodies.items()}
    evidence[original] = ArtifactEvidence(original, 'application/x-npy', path.stat().st_size, None, 'input')
    evidence[copied] = ArtifactEvidence(copied, 'application/x-npy', path.stat().st_size, 'pixels', 'output')
    if mode == 'role': evidence[root] = replace(evidence[root], role='document')
    if mode == 'source': evidence[params_ref] = replace(evidence[params_ref], source='output')
    if mode == 'size': evidence[params_ref] = replace(evidence[params_ref], bytes=float(len(params_raw)))
    if mode == 'raw_sha': bodies[params_ref] += b' '
    active = True
    authorized = True
    reads = []

    class Reader:
        def evidence(self, ref):
            nonlocal active, authorized
            assert active and authorized, 'read after authority loss'
            reads.append(ref)
            if ref == original and mode == 'revoked': active = False
            if ref == shown_ref and mode == 'authority_lost': authorized = False
            return evidence[ref]

        def read_json(self, ref, maximum):
            assert active and authorized and maximum == 1048576
            return bodies[ref]

    admissions = []
    def admit(raw):
        assert raw == params_raw
        admissions.append(raw)
        return authorized

    def run():
        return verify_shown_retouch_output(Reader(), params_ref, root,
            verify_admission=admit, check_active=lambda: active)

    if mode is None:
        verified, shown, adopted = run()
        assert verified == params
        assert shown.pixels.sha256 == adopted.pixels.sha256
        assert len(admissions) > 2 and reads[-2:] == [original, copied]
    else:
        with pytest.raises(AdoptionInvalid): run()
        if mode in ('source', 'size', 'raw_sha', 'foreign_params'): assert not admissions
        if mode in ('revoked', 'authority_lost'): assert copied not in reads


def test_retouch_copy_requires_live_context_before_io():
    class NoIO:
        def evidence(self, ref): raise AssertionError('unexpected IO')
    with pytest.raises(AdoptionInvalid, match='CONTEXT_REQUIRED'):
        verify_shown_retouch_output(NoIO(), None, None, verify_admission=None, check_active=lambda: True)
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        verify_shown_retouch_output(NoIO(), None, None, verify_admission=lambda raw: True, check_active=lambda: False)
