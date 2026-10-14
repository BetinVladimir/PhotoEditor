"""Bind current qualification reports to imported model-bearing documents.

All documents still pass the independent operator semantic callback. This
layer adds live report and model-lock checks; it never treats archived claims
or a passing report as consent or source provenance.
"""

from dataclasses import dataclass
from copy import deepcopy
from uuid import UUID

from pydantic import BaseModel

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
                 check_active, person_current=None):
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
        if 'mask.person' in owned and not callable(person_current):
            raise ValueError('INTERCHANGE_IMPORT_RUNTIME_PERSON_AUTHORITY')
        self.catalog, self.objects = catalog, objects
        self.policies, self.other, self.active = owned, qualify_other, check_active
        self.person_current = person_current

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

    def _person_masks(self, scope, context, document, binding):
        """Require a live report for every nested whole-person mask.

        Document contracts are typed before this walk. The node limit also
        guards an operator-installed extra validator returning a cyclic graph.
        """
        stack, visited, pins = [document], set(), {}
        nodes = 0
        while stack:
            current = stack.pop()
            if not isinstance(current, (BaseModel, dict, list, tuple)):
                continue
            identity = id(current)
            if identity in visited:
                continue
            visited.add(identity)
            nodes += 1
            if nodes > 10000:
                raise AdoptionInvalid('INTERCHANGE_IMPORT_RUNTIME_DOCUMENT_LIMIT')
            if isinstance(current, MaskRef) and current.selector == 'person':
                if current.object_id is not None or current.model_lock_id is None:
                    return False
                pin = (current.artifact.artifact_id, current.artifact.sha256,
                       current.model_lock_id)
                pins[pin] = current
            if isinstance(current, BaseModel):
                stack.extend(getattr(current, name)
                             for name in type(current).model_fields)
            elif isinstance(current, dict):
                stack.extend(current.values())
            else:
                stack.extend(current)
        for pin, mask in sorted(pins.items()):
            if self.person_current is None:
                raise AdoptionInvalid('INTERCHANGE_IMPORT_PERSON_AUTHORITY_UNAVAILABLE')
            before = self.person_current(scope, context, deepcopy(mask),
                                         binding.model_copy(deep=True))
            if type(before) is not int or before < 1:
                raise AdoptionInvalid('INTERCHANGE_IMPORT_PERSON_AUTHORITY_UNAVAILABLE')
            model_lock = pin[2]
            if not self._report(scope, 'mask.person', model_lock):
                return False
            after = self.person_current(scope, context, deepcopy(mask),
                                        binding.model_copy(deep=True))
            if after != before:
                raise AdoptionInvalid('INTERCHANGE_IMPORT_PERSON_AUTHORITY_CHANGED')
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
        if not self._person_masks(scope, context, typed, binding):
            return False
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
                    or not self._report(scope, 'color.stats',
                                        self.policies.get('color.stats').expected.get('ModelLockSHA')
                                        if self.policies.get('color.stats') else None)):
                return False
        accepted = self.other(scope, context, contract, deepcopy(typed),
                              binding.model_copy(deep=True))
        self.active()
        return accepted is True
