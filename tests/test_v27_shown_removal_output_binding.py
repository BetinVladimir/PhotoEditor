"""Independent adopter document/provenance verification; no database execution."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import ArtifactRef, AdoptionInvalid
from photo_worker.v2.shown_removal_adoption import verify_shown_removal_output
from test_v27_removal_copy_complete import bundle


@pytest.mark.parametrize('failure', [None, 'noncanonical_params', 'noncanonical_output',
    'shown_revoked', 'output_revoked', 'pixel_source', 'pixel_role', 'pixel_size', 'color', 'original_evidence_revoked',
    'cancel_params_read', 'cancel_shown_read', 'cancel_output_read',
    'cancel_original_evidence', 'cancel_copied_evidence'])
def test_removal_adopter_owns_canonical_documents_and_rechecks_authority(tmp_path, failure):
    params, documents, original, shown = bundle(tmp_path, None)
    rows = {}; bodies = {}; revoked = False; cancelled = False; calls = []
    def put(value, source, role, noncanonical=False):
        raw = canonical_bytes(value) + (b' ' if noncanonical else b'')
        ref = ArtifactRef(uuid4(), sha256(raw).hexdigest())
        rows[ref.artifact_id] = SimpleNamespace(ref=ref, source=source, role=role,
            media_type='application/json', bytes=len(raw))
        bodies[ref.artifact_id] = raw
        return ref
    # Shown root must retain its canonical receipt identity.
    # V2 refs use UUID identities, unlike JSON dictionaries.
    from uuid import UUID
    shown_ref = ArtifactRef(UUID(str(params['working_image']['artifact_id'])), params['working_image']['sha256'])
    rows[shown_ref.artifact_id] = SimpleNamespace(ref=shown_ref, source='input', role=None,
        media_type='application/json', bytes=len(documents[str(shown_ref.artifact_id)]))
    bodies[shown_ref.artifact_id] = documents[str(shown_ref.artifact_id)]
    params_ref = put(params, 'input', None, failure == 'noncanonical_params')
    pixels = ArtifactRef(uuid4(), shown['pixels']['sha256'])
    output = dict(shown, pixels=dict(artifact_id=str(pixels.artifact_id), sha256=pixels.sha256))
    if failure == 'color':output['color'] = dict(output['color'], icc_sha256='0'*64)
    root = put(output, 'output', 'working_image', failure == 'noncanonical_output')
    source_pixels = ArtifactRef(UUID(shown['pixels']['artifact_id']), shown['pixels']['sha256'])
    for ref, source in ((source_pixels, 'input'), (pixels, 'output')):
        rows[ref.artifact_id] = SimpleNamespace(ref=ref, source=source, role='pixels',
            media_type='application/x-npy', bytes=original.stat().st_size)
    if failure == 'pixel_source':rows[pixels.artifact_id].source = 'input'
    if failure == 'pixel_role':rows[pixels.artifact_id].role = 'working_image'
    if failure == 'pixel_size':rows[pixels.artifact_id].bytes += 1
    class Reader:
        def evidence(self, ref):
            nonlocal revoked, cancelled
            calls.append(ref.artifact_id)
            if failure == 'original_evidence_revoked' and ref == source_pixels:
                revoked = True
            if ((failure == 'cancel_original_evidence' and ref == source_pixels)
                    or (failure == 'cancel_copied_evidence' and ref == pixels)):
                cancelled = True
            return rows[ref.artifact_id]
        def read_json(self, ref, maximum):
            nonlocal revoked, cancelled
            if (failure == 'shown_revoked' and ref == shown_ref) or (failure == 'output_revoked' and ref == root):
                revoked = True
            if ((failure == 'cancel_params_read' and ref == params_ref)
                    or (failure == 'cancel_shown_read' and ref == shown_ref)
                    or (failure == 'cancel_output_read' and ref == root)):
                cancelled = True
                return b'invalid after cancellation'
            return bodies[ref.artifact_id]
    def execute():
        return verify_shown_removal_output(Reader(), params_ref, root,
            verify_admission=lambda _: not revoked, check_active=lambda: not cancelled)
    if failure:
        with pytest.raises(AdoptionInvalid):execute()
        if failure in ('shown_revoked', 'cancel_shown_read'):
            assert root.artifact_id not in calls
        if failure == 'cancel_params_read':assert shown_ref.artifact_id not in calls
        if failure == 'cancel_output_read':assert source_pixels.artifact_id not in calls
        if failure == 'cancel_original_evidence':assert pixels.artifact_id not in calls
        if failure == 'original_evidence_revoked':assert pixels.artifact_id not in calls
    else:
        _, before, after = execute()
        assert before.alpha == after.alpha
        assert before.pixels.sha256 == after.pixels.sha256
        assert before.pixels.artifact_id != after.pixels.artifact_id
