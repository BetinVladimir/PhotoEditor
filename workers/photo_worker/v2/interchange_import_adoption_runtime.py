"""Bind current qualification reports to imported model-bearing documents.

All documents still pass the independent operator semantic callback. This
layer adds live report and model-lock checks; it never treats archived claims
or a passing report as consent or source provenance.
"""

from dataclasses import dataclass
from copy import deepcopy
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.restoration import DenoiseParams, UpscaleParams
from photo_worker.contracts.statistics import StatsParams
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid
from .interchange_import_adoption_qualification_report import validate_import_adoption_expected


@dataclass(frozen=True)
class ImportAdoptionQualification:
    expected: dict
    report_id: UUID
    report_sha: str


class SchedulerImportAdoptionRuntimeAuthority:
    """Process-selected policies plus a mandatory independent semantic gate."""

    OPERATIONS = frozenset(('image.denoise', 'image.upscale',
                            'color.stats', 'mask.person'))

    def __init__(self, catalog, objects, qualifications, qualify_other,
                 check_active):
        if (not callable(getattr(catalog, 'verified_report', None))
                or not callable(getattr(objects, 'get_object', None))
                or not isinstance(qualifications, dict)
                or not callable(qualify_other) or not callable(check_active)):
            raise ValueError('INTERCHANGE_IMPORT_RUNTIME_INSTALLATION')
        owned = {}
        for key, value in qualifications.items():
            operation = key[0] if isinstance(key, tuple) and len(key) == 2 else key
            if (operation not in self.OPERATIONS
                    or operation == 'image.upscale' and not (
                        isinstance(key, tuple) and len(key) == 2 and key[1] in (2, 4))
                    or operation != 'image.upscale' and key != operation
                    or not isinstance(value, ImportAdoptionQualification)
                    or not isinstance(value.report_id, UUID) or value.report_id.int == 0
                    or not isinstance(value.report_sha, str) or len(value.report_sha) != 64
                    or any(c not in '0123456789abcdef' for c in value.report_sha)):
                raise ValueError('INTERCHANGE_IMPORT_RUNTIME_QUALIFICATION')
            try:
                expected = parse_json(canonical_bytes(value.expected), max_bytes=1 << 20)
            except (TypeError, ValueError) as error:
                raise ValueError('INTERCHANGE_IMPORT_RUNTIME_QUALIFICATION') from error
            if (not isinstance(expected, dict) or expected.get('Operation') != operation
                    or expected.get('Version') != '1'
                    or expected.get('LicenseCleared') is not True):
                raise ValueError('INTERCHANGE_IMPORT_RUNTIME_QUALIFICATION')
            try:
                validate_import_adoption_expected(expected)
            except AdoptionInvalid as error:
                raise ValueError('INTERCHANGE_IMPORT_RUNTIME_QUALIFICATION') from error
            owned[key] = ImportAdoptionQualification(expected, value.report_id,
                                                     value.report_sha)
        self.catalog, self.objects = catalog, objects
        self.policies, self.other, self.active = owned, qualify_other, check_active

    def _report(self, scope, key, model_lock):
        policy = self.policies.get(key)
        if policy is None:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_UNAVAILABLE')
        if model_lock != policy.expected.get('ModelLockSHA'):
            return False
        operation = key[0] if isinstance(key, tuple) else key
        self.active()
        report = self.catalog.verified_report(self.objects, scope, operation,
            deepcopy(policy.expected), policy.report_id, policy.report_sha)
        self.active()
        if (not isinstance(report, dict) or report.get('operation') != operation
                or report.get('model_lock_sha') != model_lock):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_RUNTIME_REPORT_MISMATCH')
        return True

    def __call__(self, scope, context, contract, typed, binding):
        if (not isinstance(scope, tuple) or len(scope) != 5
                or getattr(context, 'operation', None) != 'v2.interchange.import'
                or getattr(context, 'version', None) != '1'
                or (getattr(context, 'attempt', None), getattr(context, 'fence', None),
                    getattr(context, 'result_id', None)) != scope[2:]
                or not isinstance(binding, VersionBinding)):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_RUNTIME_SCOPE')
        self.active()
        if contract == 'Params' and isinstance(typed, DenoiseParams):
            if typed.image != binding.source or not self._report(
                    scope, 'image.denoise', typed.model_lock_id):
                return False
        elif contract == 'Params' and isinstance(typed, UpscaleParams):
            if typed.image != binding.source or not self._report(
                    scope, ('image.upscale', typed.scale), typed.model_lock_id):
                return False
        elif contract == 'Params' and isinstance(typed, StatsParams):
            if (typed.image != binding.source
                    or typed.person.object_id is not None
                    or typed.person.model_lock_id is None
                    or not self._report(scope, 'color.stats',
                                        self.policies.get('color.stats').expected.get('ModelLockSHA')
                                        if self.policies.get('color.stats') else None)
                    or not self._report(scope, 'mask.person',
                                        typed.person.model_lock_id)):
                return False
        elif contract == 'MaskRef' and isinstance(typed, MaskRef) and typed.selector == 'person':
            if (typed.object_id is not None or typed.model_lock_id is None
                    or not self._report(scope, 'mask.person', typed.model_lock_id)):
                return False
        accepted = self.other(scope, context, contract, deepcopy(typed),
                              binding.model_copy(deep=True))
        self.active()
        return accepted is True
