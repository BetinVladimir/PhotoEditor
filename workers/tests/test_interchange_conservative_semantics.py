from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.defects import DefectCandidates, DefectProposeParams, DefectProposeReport
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_conservative_semantics import qualify_conservative_document
from test_conservative_selection import image, defect, ref


@pytest.mark.parametrize('mode', ['valid', 'candidates_without_faces', 'foreign_source', 'hash', 'wrong_contract', 'cancel'])
def test_prepared_no_face_report_requires_complete_exact_declared_source_and_empty_detector_output(mode):
    source = image()
    source_ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(source)).hexdigest())
    binding = VersionBinding(asset_version_id=uuid4(), source=source_ref, canvas_hash=canonical_hash(source.canvas))
    proposal = DefectCandidates(image=source_ref, source_pixels=source.pixels, label_map=ref(2),
        canvas=source.canvas, transform_chain=source.transform_chain, candidates=[])
    if mode == 'candidates_without_faces':
        proposal = defect()[0]; proposal.image=source_ref
        source.pixels=proposal.source_pixels
        source_ref.sha256=sha256(canonical_bytes(source)).hexdigest(); proposal.image=source_ref
    proposal_ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(proposal)).hexdigest())
    protected = MaskRef(artifact=ref(3), canvas=source.canvas, transform_chain=source.transform_chain,
        object_id=None, selector='manual', confidence=1, model_lock_id=None, protected=True)
    report = DefectProposeReport(params=DefectProposeParams(image=source_ref, faces=[]), proposal=proposal_ref,
        protection=protected, label_png=ref(4), faces=[], warnings=['no_faces'])
    documents = {source_ref.artifact_id: ('WorkingImage', canonical_bytes(source)), proposal_ref.artifact_id: ('DefectCandidates', canonical_bytes(proposal))}
    def require(reference, contract):
        if mode == 'wrong_contract' or documents[reference.artifact_id][0] != contract: raise AdoptionInvalid('contract')
    def read(reference, maximum):
        raw=documents[reference.artifact_id][1]
        return b'{}' if mode == 'hash' else raw
    def active():
        if mode == 'cancel': raise RuntimeError('cancelled')
    if mode == 'foreign_source': report.params.image=ref(5)
    if mode in ('hash', 'wrong_contract', 'cancel'):
        with pytest.raises((AdoptionInvalid, RuntimeError)):
            qualify_conservative_document('DefectProposeReport', report, binding, read, require, active)
    else:
        assert qualify_conservative_document('DefectProposeReport', report, binding, read, require, active) is (mode == 'valid')
