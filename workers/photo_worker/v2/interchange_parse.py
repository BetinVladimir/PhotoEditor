"""PhotoEditor bundle parse/staging phase; outputs stay untrusted until ingest."""
from contextlib import contextmanager
from dataclasses import dataclass
from typing import Annotated, Callable, Literal
import zipfile

from pydantic import Field

from photo_worker.contracts.base import ArtifactRef, Contract, canonical_bytes
from .interchange_archive import ArchiveLimits, interchange_path, verify_archive
from .interchange_manifest import BundleManifest, verify_bundle_manifest


class ParsedFile(Contract):
    path: Annotated[str, Field(min_length=1, max_length=240)]
    content: ArtifactRef
    contract: Annotated[str, Field(min_length=1, max_length=120)]


class ParsedInterchange(Contract):
    schema_version: Literal[1]
    manifest: BundleManifest
    files: Annotated[list[ParsedFile], Field(min_length=1, max_length=30000)]


@dataclass(frozen=True)
class StagedBundleParse:
    document: ParsedInterchange
    outputs: tuple[ArtifactRef, ...]
    bytes_written: int


def validate_parsed_inventory(parsed: ParsedInterchange) -> None:
    """Bind gateway-issued file refs to every untrusted manifest inventory row."""
    listed_paths = [interchange_path(item.path) for item in parsed.manifest.inventory]
    observed_paths = [interchange_path(item.path) for item in parsed.files]
    if (len({path.casefold() for path in listed_paths}) != len(listed_paths)
            or len({path.casefold() for path in observed_paths}) != len(observed_paths)
            or 'manifest.json' in {path.casefold() for path in listed_paths}
            or len({item.artifact_id for item in parsed.manifest.inventory}) != len(listed_paths)
            or any(item.artifact_id.int == 0 for item in parsed.manifest.inventory)):
        raise ValueError('INTERCHANGE_PARSED_INVENTORY')
    listed = {item.path: item for item in parsed.manifest.inventory}
    observed = {item.path: item for item in parsed.files}
    if (len(listed) != len(parsed.manifest.inventory) or len(observed) != len(parsed.files)
            or set(listed) != set(observed)
            or len({item.content.artifact_id for item in parsed.files}) != len(parsed.files)):
        raise ValueError('INTERCHANGE_PARSED_INVENTORY')
    for path, file in observed.items():
        item = listed[path]
        if (file.content.artifact_id.int == 0 or file.content.sha256 != item.sha256
                or file.contract != item.contract):
            raise ValueError('INTERCHANGE_PARSED_INVENTORY')


def stage_photoeditor_bundle(artifacts, bundle: ArtifactRef, limits: ArchiveLimits,
                             active: Callable[[], None]) -> StagedBundleParse:
    """Use declared-input materialization and attempt-owned numeric scratch names.

    All archive bytes/inventory are verified before first upload. Original UUIDs
    and contract labels in manifest are retained as untrusted source metadata;
    only gateway-issued refs identify staged imported files. Caller must admit
    current lease/scope before and after this phase and publish the returned
    document/root in its fenced completion transaction.
    """
    active()
    with artifacts.input(bundle, suffix=".zip") as materialized:
        with materialized.path.open("rb") as source:
            inventory = verify_archive(source, limits, cancelled=lambda: _cancelled(active))
            metadata = next((item for item in inventory if item.path == "manifest.json"), None)
            if metadata is None or metadata.bytes > 8 << 20:
                raise ValueError("missing or oversized bundle manifest")
            with zipfile.ZipFile(source, "r") as archive:
                with archive.open("manifest.json") as content:
                    raw = content.read((8 << 20) + 1)
            manifest = verify_bundle_manifest(raw, inventory)
            active()
            # Reserve the actual verified expansion, not ZIP size declarations.
            with artifacts.scratch.reserve(sum(item.bytes for item in inventory)) as reservation:
                paths = {}

                @contextmanager
                def stage(path):
                    active()
                    local = reservation.path("imported", ".bin")
                    paths[path] = local
                    with artifacts.scratch.open_writer(local, reservation) as output:
                        yield output

                repeated = verify_archive(source, limits, stage=stage,
                                          cancelled=lambda: _cancelled(active))
                if repeated != inventory:
                    raise ValueError("bundle changed during staging")
                parsed_files = []
                outputs = []
                output_ids = set()
                written = 0
                observed = {item.path: item for item in inventory}
                for entry in sorted(manifest.inventory, key=lambda item: item.path):
                    active()
                    ref = artifacts.stage(paths[entry.path], reservation, "imported_file",
                                          "application/octet-stream")
                    if ref.artifact_id.int == 0 or ref.sha256 != observed[entry.path].sha256:
                        raise ValueError("staged imported file hash mismatch")
                    if ref.artifact_id in output_ids:
                        raise ValueError("duplicate staged imported identity")
                    output_ids.add(ref.artifact_id)
                    outputs.append(ref.model_copy(deep=True))
                    written += observed[entry.path].bytes
                    parsed_files.append(ParsedFile(path=entry.path, content=ref.model_copy(deep=True),
                                                   contract=entry.contract))
                result = ParsedInterchange(schema_version=1, manifest=manifest,
                                           files=parsed_files)
                validate_parsed_inventory(result)
                if len(canonical_bytes(result)) > 8 << 20:
                    raise ValueError("parsed interchange output exceeds document budget")
                active()
                return StagedBundleParse(result, tuple(outputs), written)


def _cancelled(active: Callable[[], None]) -> bool:
    active()
    return False
