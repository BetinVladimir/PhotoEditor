"""Concrete sidecar preparation with canonical masters and physical images."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.v2.interchange_export_sidecars import prepare_export_sidecars
from photo_worker.v2.interchange_mapping import MappingEntry
from photo_worker.v2.interchange_plan import InterchangePlan
from .export_adapter import ExportAuthority
from .export_adapter import PreparedExportWork
from .export_images import qualify_export_image


class SidecarExportPreparationFactory:
    def __init__(self, work_icc_sha):
        if (not isinstance(work_icc_sha, str) or len(work_icc_sha) != 64
                or any(c not in '0123456789abcdef' for c in work_icc_sha)):
            raise ValueError('INTERCHANGE_EXPORT_ICC_PIN')
        self.icc = work_icc_sha

    def __call__(self, ctx, artifacts, authority, plan, reader):
        ctx.check_active()
        plan = InterchangePlan.model_validate_json(canonical_bytes(plan))
        if (not isinstance(authority, ExportAuthority)
                or plan.direction != 'export'
                or plan.project_id != authority.project_id
                or plan.snapshot_digest != authority.snapshot_digest
                or type(authority.max_archive_bytes) is not int
                or not 1 <= authority.max_archive_bytes <= 64 << 30
                or plan.max_archive_bytes > authority.max_archive_bytes):
            raise ValueError('INTERCHANGE_EXPORT_FACTORY_AUTHORITY')
        if plan.format not in ('xmp', 'captureone_sidecar'):
            raise ValueError('INTERCHANGE_EXPORT_FACTORY_FORMAT')
        masters = {}
        def qualify(entry):
            ctx.check_active()
            binding = entry.binding
            key = (binding.source.artifact_id, binding.source.sha256)
            declared = reader.entries.get(key)
            if declared is None or declared.contract != 'WorkingImage':
                raise ValueError('INTERCHANGE_EXPORT_MASTER_CONTRACT')
            if key not in masters:
                raw = reader.read_json(binding.source, min(1 << 20, reader.read_budget.remaining_json_bytes))
                image = WorkingImage.model_validate_json(raw)
                if (canonical_bytes(image) != raw or sha256(raw).hexdigest() != binding.source.sha256
                        or image.dtype != 'float32' or image.color.profile_id != 'work_rec2020_gamma24_v1'
                        or image.color.encoding != 'gamma24' or image.color.icc_sha256 != self.icc):
                    raise ValueError('INTERCHANGE_EXPORT_MASTER_INVALID')
                masters[key] = image
            if canonical_hash(masters[key].canvas) != binding.canvas_hash:
                raise ValueError('INTERCHANGE_EXPORT_MASTER_CANVAS')
            return qualify_export_image(entry, reader, artifacts.scratch, active=ctx.check_active)
        prepared = prepare_export_sidecars(plan, qualify_image=qualify, active=ctx.check_active)
        reader.recheck()
        return PreparedExportWork(prepared, tuple(MappingEntry.model_validate_json(raw) for raw in prepared.mapping_jcs))
