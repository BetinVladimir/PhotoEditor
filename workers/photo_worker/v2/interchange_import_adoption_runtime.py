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
from photo_worker.contracts.conservative import DefectAutoRepairParams, DustAutoRepairParams
from photo_worker.contracts.defects import DefectProposeReport
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
                            'color.stats', 'mask.person', 'defects.auto_repair', 'sensor.dust_auto_repair'))

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
                    or expected.get('LicenseCleared') is not True
                    or operation in ('defects.auto_repair','sensor.dust_auto_repair') and expected.get('Backend') != 'cpu'):
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

    def _person_masks(self, scope, context, document, binding, observed=None):
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
            if observed is not None:
                key = (binding.asset_version_id,) + pin
                previous = observed.get(key)
                if previous is not None and previous[2] != after:
                    raise AdoptionInvalid('INTERCHANGE_IMPORT_PERSON_AUTHORITY_CHANGED')
                observed[key] = (deepcopy(mask), binding.model_copy(deep=True), after)
        return True

    def bound_scope(self, scope, context):
        """Return the task-local qualifier and final live person recheck."""
        observed = {}
        def qualify(contract, typed, binding):
            return self._qualify(scope, context, contract, typed, binding, observed)
        def recheck():
            for mask, binding, epoch in observed.values():
                self.active()
                current = self.person_current(scope, context, deepcopy(mask),
                                              binding.model_copy(deep=True))
                self.active()
                if current != epoch:
                    raise AdoptionInvalid('INTERCHANGE_IMPORT_PERSON_AUTHORITY_CHANGED')
        return qualify, recheck

    def __call__(self, scope, context, contract, typed, binding):
        return self._qualify(scope, context, contract, typed, binding, None)

    def _qualify(self, scope, context, contract, typed, binding, observed):
        if (not isinstance(scope, tuple) or len(scope) != 5
                or getattr(context, 'operation', None) != 'v2.interchange.import'
                or getattr(context, 'version', None) != '1'
                or (getattr(context, 'attempt', None), getattr(context, 'fence', None),
                    getattr(context, 'result_id', None)) != scope[2:]
                or not isinstance(binding, VersionBinding)):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_RUNTIME_SCOPE')
        self.active()
        if not self._person_masks(scope, context, typed, binding, observed):
            return False
        if contract == 'Params' and isinstance(typed, (DefectAutoRepairParams, DustAutoRepairParams)):
            operation = 'defects.auto_repair' if isinstance(typed, DefectAutoRepairParams) else 'sensor.dust_auto_repair'
            policy = self.policies.get(operation)
            if (typed.image != binding.source or policy is None
                    or not self._report(scope, operation, policy.expected.get('ModelLockSHA'))):
                return False
        elif contract == 'DefectProposeReport':
            # This report determines eligibility/protection, so its algorithm
            # must be covered by the same measured pinned native engine.
            policy = self.policies.get('defects.auto_repair')
            if (typed.params.image != binding.source or typed.algorithm != 'defect_dog_v1'
                    or policy is None or not self._report(scope, 'defects.auto_repair', policy.expected.get('ModelLockSHA'))):
                return False
        elif contract == 'Params' and isinstance(typed, DenoiseParams):
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
