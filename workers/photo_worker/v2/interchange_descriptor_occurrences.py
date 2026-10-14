"""Exact descriptor occurrences qualified against every referring frozen root."""
from copy import deepcopy
from hashlib import sha256
from uuid import UUID
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import DescriptorShard, FaceObservations, VersionBinding
from .adoption import AdoptionInvalid
from .render_recipe import decode_frozen_render_recipe


class PreparedDescriptorOccurrenceQualifier:
    def __init__(self, *, pins, qualify_current, qualify_other, check_active=lambda: None):
        if not all(callable(port) for port in (qualify_current, qualify_other, check_active)):
            raise ValueError('INTERCHANGE_DESCRIPTOR_INSTALLATION')
        self.current, self.other, self.active = qualify_current, qualify_other, check_active
        self.pins = []
        identities = {}
        required = {'root', 'root_body', 'descriptor', 'descriptor_body', 'selected_binding',
                    'analysis_id', 'selected_ordinal', 'model_lock_sha256'}
        if not isinstance(pins, (list, tuple)) or len(pins) > 5000:
            raise ValueError('INTERCHANGE_DESCRIPTOR_PINS')
        for supplied in pins:
            self.active()
            if not isinstance(supplied, dict) or set(supplied) != required:
                raise ValueError('INTERCHANGE_DESCRIPTOR_PIN')
            pin = deepcopy(supplied)
            try:
                for name in ('root', 'descriptor'):
                    pin[name] = ArtifactRef.model_validate_json(canonical_bytes(pin[name]))
                    if pin[name].artifact_id.int == 0:
                        raise ValueError('INTERCHANGE_DESCRIPTOR_PIN')
                    identity = (pin[name].sha256, name)
                    old = identities.setdefault(pin[name].artifact_id, identity)
                    if old != identity:
                        raise ValueError('INTERCHANGE_DESCRIPTOR_PIN_CONFLICT')
                pin['selected_binding'] = VersionBinding.model_validate_json(canonical_bytes(pin['selected_binding']))
                pin['analysis_id'] = UUID(str(pin['analysis_id']))
                lock = pin['model_lock_sha256']
                if (pin['analysis_id'].int == 0 or type(pin['selected_ordinal']) is not int
                        or not 0 <= pin['selected_ordinal'] < 5000 or not isinstance(lock, str)
                        or len(lock) != 64 or any(c not in '0123456789abcdef' for c in lock)):
                    raise ValueError('INTERCHANGE_DESCRIPTOR_PIN')
                for name, maximum in (('root', 1 << 20), ('descriptor', 8 << 20)):
                    raw = pin[name + '_body']
                    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= maximum
                            or sha256(raw).hexdigest() != pin[name].sha256):
                        raise ValueError('INTERCHANGE_DESCRIPTOR_PIN_BYTES')
                shard = DescriptorShard.model_validate_json(pin['descriptor_body'])
                if (canonical_bytes(shard) != pin['descriptor_body']
                        or len({frame.ordinal for frame in shard.frames}) != len(shard.frames)):
                    raise ValueError('INTERCHANGE_DESCRIPTOR_PIN_FRAMES')
                selected = [frame for frame in shard.frames if frame.ordinal == pin['selected_ordinal']]
                if len(selected) != 1 or selected[0].binding != pin['selected_binding']:
                    raise ValueError('INTERCHANGE_DESCRIPTOR_PIN_SELECTION')
                root = decode_frozen_render_recipe(pin['root_body'])
                if (root.learning is None or root.learning.descriptor != pin['descriptor']
                        or root.learning.analysis_id != pin['analysis_id']
                        or root.learning.ordinal != pin['selected_ordinal']
                        or root.scalars.source != pin['selected_binding']
                        or root.scalars.model_lock_sha != lock):
                    raise ValueError('INTERCHANGE_DESCRIPTOR_PIN_ROOT')
            except (TypeError, AttributeError, KeyError) as error:
                raise ValueError('INTERCHANGE_DESCRIPTOR_PIN') from error
            self.pins.append((pin, shard))
        self.active()

    def _authorize(self, event, pin, frame=None):
        evidence = deepcopy(pin)
        evidence['frame_binding'] = None if frame is None else frame.binding.model_copy(deep=True)
        evidence['frame_ordinal'] = None if frame is None else frame.ordinal
        self.active()
        result = self.current(deepcopy(event), evidence)
        self.active()
        if result is not True:
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_AUTHORITY')

    def _other(self, event):
        self.active()
        result = self.other(deepcopy(event))
        self.active()
        if result is not True:
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_OTHER_AUTHORITY')
        return True

    def __call__(self, occurrence):
        self.active()
        event = deepcopy(occurrence)
        contract, parent_contract = event.get('contract'), event.get('parent_contract')
        reference, parent, path = event.get('reference'), event.get('parent'), event.get('path')
        descriptor_matches = [(pin, shard) for pin, shard in self.pins if pin['descriptor'] == reference]
        if contract == 'DescriptorShard' or descriptor_matches:
            matches = [(pin, shard) for pin, shard in descriptor_matches if pin['root'] == parent]
            if contract != 'DescriptorShard' or not matches or parent_contract != 'FrozenRenderRecipe' or path != ('learning', 'descriptor'):
                raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_ROOT')
            for pin, _ in matches:
                if canonical_bytes(event.get('parent_body')) != pin['root_body']:
                    raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_ROOT_BYTES')
                self._authorize(event, pin)
            return True
        parents = [(pin, shard) for pin, shard in self.pins if pin['descriptor'] == parent]
        if parents or parent_contract == 'DescriptorShard':
            if not parents or parent_contract != 'DescriptorShard':
                raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_PARENT')
            found = False
            for pin, shard in parents:
                if canonical_bytes(event.get('parent_body')) != pin['descriptor_body']:
                    raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_PARENT_BYTES')
                frames = [frame for frame in shard.frames if
                    (contract == 'WorkingImage' and path == ('frames', '*', 'binding', 'source') and frame.binding.source == reference)
                    or (contract == 'FaceObservations' and path == ('frames', '*', 'faces') and frame.faces == reference)]
                if not frames:
                    raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_FRAME')
                for frame in frames:
                    self._authorize(event, pin, frame)
                    found = True
            return found
        if parent_contract in ('WorkingImage', 'FaceObservations'):
            matches = [(pin, frame) for pin, shard in self.pins for frame in shard.frames
                       if (parent_contract == 'WorkingImage' and frame.binding.source == parent)
                       or (parent_contract == 'FaceObservations' and frame.faces == parent)]
            if not matches:
                if parent_contract == 'FaceObservations':
                    raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_UNPINNED_FACE')
                return self._other(event)
            raw = canonical_bytes(event.get('parent_body'))
            if parent is None or sha256(raw).hexdigest() != parent.sha256:
                raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_DESCENDANT_BYTES')
            try:
                body = (WorkingImage if parent_contract == 'WorkingImage' else FaceObservations).model_validate_json(raw)
            except ValueError as error:
                raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_DESCENDANT_SCHEMA') from error
            for pin, frame in matches:
                if parent_contract == 'FaceObservations':
                    valid = (path == ('binding', 'source') and contract == 'WorkingImage'
                             and body.binding == frame.binding and reference == frame.binding.source)
                else:
                    valid = canonical_hash(body.canvas) == frame.binding.canvas_hash and (
                        (path == ('pixels',) and contract == 'WorkingPixels' and reference == body.pixels)
                        or (path == ('alpha',) and contract == 'WorkingAlpha' and body.alpha is not None and reference == body.alpha)
                        or (path == ('transform_chain', 'steps', '*', 'inverse_field') and contract == 'DenseWarpField'
                            and any(getattr(step, 'inverse_field', None) == reference for step in body.transform_chain.steps)))
                if not valid:
                    raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_DESCENDANT')
                self._authorize(event, pin, frame)
            return True
        if contract == 'FaceObservations':
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_UNPINNED_FACE')
        return self._other(event)


class SQLExportDescriptorAuthority:
    """Current lease/model/consent checks; bind separately for each export task."""
    def __init__(self, database, ctx, params):
        from .interchange_plan import InterchangeExportParams
        if not callable(getattr(database, 'interchange_export_descriptor_authority', None)):
            raise ValueError('INTERCHANGE_DESCRIPTOR_SQL_INSTALLATION')
        typed = InterchangeExportParams.model_validate_json(canonical_bytes(params))
        raw = canonical_bytes(typed)
        command = ctx.lease.command
        if (command.operation != 'v2.interchange.export' or command.operation_version != '1'
                or command.resource_class != 'cpu' or sha256(raw).hexdigest() != command.params.sha256):
            raise ValueError('INTERCHANGE_DESCRIPTOR_SQL_INSTALLATION')
        self.database, self.ctx, self.params = database, ctx, raw

    def __call__(self, occurrence, evidence):
        from photo_worker.contracts.base import parse_json
        self.ctx.check_active()
        wire = dict(root=evidence['root'], root_jcs=evidence['root_body'].decode('utf-8'),
            descriptor=evidence['descriptor'], selected_binding=evidence['selected_binding'],
            analysis_id=evidence['analysis_id'], selected_ordinal=evidence['selected_ordinal'],
            model_lock_sha256=evidence['model_lock_sha256'], reference=occurrence['reference'],
            contract=occurrence['contract'], frame_binding=evidence['frame_binding'],
            frame_ordinal=evidence['frame_ordinal'])
        owned = parse_json(canonical_bytes(wire), max_bytes=2 << 20)
        allowed = self.database.interchange_export_descriptor_authority(self.ctx.lease, self.params, owned)
        self.ctx.check_active()
        return allowed is True
