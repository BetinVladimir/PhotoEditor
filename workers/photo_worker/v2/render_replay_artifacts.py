"""Read-only TaskArtifacts bridge for independently reconstructed render pixels."""
from contextlib import contextmanager
from dataclasses import dataclass
from io import BytesIO
from pathlib import Path
from .adoption import AdoptionInvalid, MAX_JSON_BYTES
from .lens_bypass import _ref
from .render_replay_reader import RenderReplayReader
from photo_worker.contracts.base import Contract


@dataclass(frozen=True)
class ReplayInputFile:
    path: Path


class RenderReplayArtifacts:
    """Retain bounded verified input files under caller-owned scratch quotas.

    No stage/commit/upload interface exists. Installed provider runtime/model
    qualification and total RAM/VRAM/resource admission remain prerequisites.
    Scratch reservations and mappings must remain owned by their surrounding
    preparation scopes, just as in the original worker algorithms.
    """
    def __init__(self, reader, scratch, check_active=lambda: None):
        if not isinstance(reader, RenderReplayReader):
            raise ValueError('Render replay requires an input-only reader')
        self.reader=reader
        self.scratch=scratch
        self.check_active=check_active

    @contextmanager
    def stream(self, ref):
        self.check_active()
        raw=self.reader.read_json(_ref(ref),1048576)
        with BytesIO(raw) as stream:
            yield stream
            self.check_active()

    def contract(self, ref, model):
        self.check_active()
        if not isinstance(model,type) or not issubclass(model,Contract):
            raise AdoptionInvalid('RENDER_REPLAY_CONTRACT_INVALID')
        raw=self.reader.read_json(_ref(ref),MAX_JSON_BYTES)
        try:result=model.model_validate_json(raw)
        except ValueError as error:
            raise AdoptionInvalid('RENDER_REPLAY_CONTRACT_INVALID') from error
        self.check_active()
        return result

    @contextmanager
    def input(self, ref, *, suffix='.npy'):
        self.check_active()
        if suffix!='.npy':
            raise AdoptionInvalid('RENDER_REPLAY_FILE_INVALID')
        reference=_ref(ref);evidence=self.reader.evidence(reference)
        if evidence.media_type!='application/x-npy':
            raise AdoptionInvalid('RENDER_REPLAY_FILE_INVALID')
        with self.scratch.reserve(evidence.bytes) as owner:
            path=owner.path('render-replay-input',suffix)
            iterator=iter(self.reader.iter_bytes(reference,evidence.bytes))
            try:
                with self.scratch.open_writer(path,owner) as target:
                    for block in iterator:
                        self.check_active();target.write(block);owner.validate()
                owner.validate_file(path)
                self.check_active()
                yield ReplayInputFile(path)
                self.check_active()
            finally:
                close=getattr(iterator,'close',None)
                if close is not None:close()
