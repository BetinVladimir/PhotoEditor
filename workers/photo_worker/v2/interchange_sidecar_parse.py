"""Quota-bound sidecar archive staging, independent of workflow admission."""
from collections.abc import Mapping
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import PurePosixPath

from photo_worker.contracts.base import canonical_bytes
from .interchange_archive import verify_archive
from .interchange_parse import ParsedFile, ParsedInterchange, _cancelled, validate_parsed_inventory
from .interchange_sidecar_import import normalize_sidecar_inventory


@dataclass(frozen=True)
class StagedSidecarParse:
    document: ParsedInterchange
    outputs: tuple
    bytes_written: int
    unsupported: tuple[tuple[str, int, str], ...]


def stage_sidecar_archive(artifacts, bundle, limits, *, format, active):
    active()
    if format not in ('xmp', 'captureone_sidecar'):
        raise ValueError('INTERCHANGE_SIDECAR_FORMAT')
    bundle = bundle.model_copy(deep=True)
    with artifacts.input(bundle.model_copy(deep=True), suffix='.zip') as materialized:
        active()
        with materialized.path.open('rb') as source:
            inventory = verify_archive(source, limits, cancelled=lambda: _cancelled(active))
            if not 2 <= len(inventory) <= 10001:
                raise ValueError('INTERCHANGE_SIDECAR_INVENTORY')
            for item in inventory:
                if PurePosixPath(item.path).suffix.lower() == '.xmp' and item.bytes > 1 << 20:
                    raise ValueError('INTERCHANGE_SIDECAR_CONTENT')
            active()
            with artifacts.scratch.reserve(sum(item.bytes for item in inventory)) as reservation:
                paths = {}

                @contextmanager
                def extract(path):
                    active()
                    local = reservation.path('sidecar-import', '.bin')
                    paths[path] = local
                    with artifacts.scratch.open_writer(local, reservation) as output:
                        yield output

                repeated = verify_archive(source, limits, stage=extract,
                    cancelled=lambda: _cancelled(active))
                if repeated != inventory:
                    raise ValueError('INTERCHANGE_SIDECAR_CHANGED')
                sidecar_paths = tuple(item.path for item in inventory
                    if PurePosixPath(item.path).suffix.lower() == '.xmp')

                class Sidecars(Mapping):
                    def __iter__(self):
                        return iter(sidecar_paths)

                    def __len__(self):
                        return len(sidecar_paths)

                    def __getitem__(self, path):
                        if path not in sidecar_paths:
                            raise KeyError(path)
                        active()
                        reservation.validate_file(paths[path])
                        with paths[path].open('rb') as content:
                            raw = content.read((1 << 20) + 1)
                        active()
                        return raw

                manifest_raw = None
                if 'manifest.json' in paths:
                    metadata = next(item for item in inventory if item.path == 'manifest.json')
                    if not 2 <= metadata.bytes <= 8 << 20:
                        raise ValueError('INTERCHANGE_SIDECAR_MANIFEST_BUDGET')
                    active()
                    with paths['manifest.json'].open('rb') as content:
                        manifest_raw = content.read((8 << 20) + 1)
                    active()
                normalized = normalize_sidecar_inventory(bundle, inventory, Sidecars(),
                    format=format, active=active, manifest_raw=manifest_raw)
                observed = {item.path: item for item in inventory}
                files, outputs, identities = [], [], set()
                written = 0
                for item in sorted(normalized.manifest.inventory, key=lambda item: item.path):
                    active()
                    ref = artifacts.stage(paths[item.path], reservation, 'imported_file',
                        'application/octet-stream')
                    active()
                    if ref.artifact_id.int == 0 or ref.artifact_id in identities or ref.sha256 != observed[item.path].sha256:
                        raise ValueError('INTERCHANGE_SIDECAR_UPLOAD')
                    identities.add(ref.artifact_id)
                    outputs.append(ref.model_copy(deep=True))
                    files.append(ParsedFile(path=item.path, content=ref.model_copy(deep=True), contract=item.contract))
                    written += observed[item.path].bytes
                document = ParsedInterchange(schema_version=1, manifest=normalized.manifest, files=files)
                validate_parsed_inventory(document)
                if len(canonical_bytes(document)) > 8 << 20:
                    raise ValueError('INTERCHANGE_SIDECAR_DOCUMENT_BUDGET')
                active()
                return StagedSidecarParse(document, tuple(outputs), written, normalized.unsupported)
