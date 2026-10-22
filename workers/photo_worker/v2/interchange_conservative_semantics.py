"""Bounded conservative document cross-checks on prepared declared inputs.

Face-bearing defect proposals require a separate authenticated ancestry/model
installation. The native path cannot manufacture faces, source ancestry or
human review. Physical arrays/PNG remain independently gated.
"""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.conservative import DefectAutoRepairParams, DustAutoRepairParams
from photo_worker.contracts.defects import DefectCandidates, DefectProposeReport
from photo_worker.contracts.dust import DustCandidates
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.algorithms.effective import resolve_effective_params
from .adoption import AdoptionInvalid


def qualify_conservative_document(contract, typed, binding, read_document, require_contract, active, qualify_faces=None):
    """Return None for an unrelated document; never grant runtime qualification."""
    if not (contract == 'Params' and isinstance(typed, (DefectAutoRepairParams, DustAutoRepairParams))
            or contract == 'DefectProposeReport' and isinstance(typed, DefectProposeReport)):
        return None
    def read(reference, name, model):
        active(); require_contract(reference, name)
        raw = read_document(reference, 8 << 20)
        if not isinstance(raw, bytes) or not 2 <= len(raw) <= 8 << 20 or sha256(raw).hexdigest() != reference.sha256:
            raise AdoptionInvalid('INTERCHANGE_CONSERVATIVE_DOCUMENT_HASH')
        value = model.model_validate_json(raw)
        if canonical_bytes(value) != raw: raise AdoptionInvalid('INTERCHANGE_CONSERVATIVE_CANONICAL')
        active(); return value
    image_ref = typed.params.image if isinstance(typed, DefectProposeReport) else typed.image
    if image_ref != binding.source: return False
    image = read(image_ref, 'WorkingImage', WorkingImage)
    if isinstance(typed, DefectProposeReport):
        report = typed
        proposal_ref = report.proposal
    else:
        resolved = read(typed.resolved, 'ResolvedStyle', ResolvedStyle)
        if resolved.params.image != binding.source or resolved.mode == 'skip': return False
        if isinstance(typed, DustAutoRepairParams):
            restoration = resolved.modules.extensions.restoration
            if restoration is None or restoration.sensor_dust != 'conservative_auto': return False
            if any(not mask.protected or mask.canvas != image.canvas or mask.transform_chain != image.transform_chain for mask in typed.protected): return False
            if typed.proposal is not None:
                proposal = read(typed.proposal, 'DustCandidates', DustCandidates)
                if image_ref not in proposal.frames: return False
            active(); return True
        defects = resolve_effective_params(resolved).modules.defects
        if not defects.enabled or defects.policy != 'conservative_auto' or not defects.preserve_permanent_marks: return False
        report = read(typed.proposal_report, 'DefectProposeReport', DefectProposeReport)
        if report.proposal != typed.proposal or report.params.image != image_ref: return False
        proposal_ref = typed.proposal
    proposal = read(proposal_ref, 'DefectCandidates', DefectCandidates)
    if (proposal.image != image_ref or proposal.source_pixels != image.pixels
            or proposal.canvas != image.canvas or proposal.transform_chain != image.transform_chain
            or report.algorithm != 'defect_dog_v1' or report.params.image != image_ref
            or not report.protection.protected or report.protection.canvas != image.canvas
            or report.protection.transform_chain != image.transform_chain): return False
    # Face-bearing reports require independent original detector ancestry.
    # No-face reports cannot legitimately contain detector candidates.
    if report.params.faces or report.faces or proposal.candidates:
        from .interchange_conservative_faces import qualify_conservative_faces
        return qualify_conservative_faces(report, proposal, image, binding, read, qualify_faces, active)
    active(); return True
