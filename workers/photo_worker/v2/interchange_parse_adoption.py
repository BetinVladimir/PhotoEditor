"""Independent physical reconstruction of an untrusted parsed bundle result."""
from dataclasses import dataclass
from hashlib import sha256
import zipfile
from collections.abc import Mapping
from pathlib import PurePosixPath
from .interchange_sidecar_import import normalize_sidecar_inventory

from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.interchange.parse import InterchangeParseParams
from .adoption import AdoptionInvalid, ArtifactRef, PreparedAdoption, require_adoption_active
from .interchange_archive import ArchiveLimits, verify_archive
from .interchange_manifest import verify_bundle_manifest
from .interchange_parse import ParsedInterchange, validate_parsed_inventory


@dataclass(frozen=True)
class VerifiedParsedInterchange:
    params_jcs: bytes
    result_jcs: bytes
    bytes_written: int


def verify_parsed_interchange(reader, params_ref, root, scratch, *, check_active=lambda: None):
    """No canonical ingest: original UUIDs/contract strings remain untrusted.

    Caller owns bounded scratch and authoritative fenced evidence reader. This
    reconstructs the source ZIP and hashes every output object independently;
    later adoption SQL must recheck current metadata/source authority atomically.
    """
    supplied_check_active = check_active
    def check_active():
        require_adoption_active(supplied_check_active)

    def ref(value):
        return ArtifactRef(value.artifact_id, value.sha256)

    def evidence(value, source, media, role=None):
        value = ref(value)
        check_active()
        item = reader.evidence(value)
        check_active()
        if (item.ref != value or item.source != source or item.media_type != media
                or type(item.bytes) is not int or item.bytes < 1
                or source == "output" and item.role != role):
            raise AdoptionInvalid("INTERCHANGE_EVIDENCE_INVALID")
        return item

    def read_json(value, source, maximum):
        item = evidence(value, source, "application/json", "document")
        if item.bytes > maximum:
            raise AdoptionInvalid("INTERCHANGE_DOCUMENT_LIMIT")
        check_active()
        raw = reader.read_json(ref(value), maximum)
        check_active()
        if not isinstance(raw, bytes) or len(raw) != item.bytes or sha256(raw).hexdigest() != value.sha256:
            raise AdoptionInvalid("INTERCHANGE_DOCUMENT_HASH")
        check_active()
        return raw

    def stream(value, item, maximum, writer=None):
        count, digest = 0, sha256()
        check_active()
        iterator = reader.iter_bytes(ref(value), maximum)
        try:
            for block in iterator:
                check_active()
                if not isinstance(block, bytes) or not block or len(block) > 1 << 20:
                    raise AdoptionInvalid("INTERCHANGE_STREAM_INVALID")
                count += len(block)
                if count > item.bytes or count > maximum:
                    raise AdoptionInvalid("INTERCHANGE_STREAM_LIMIT")
                digest.update(block)
                if writer is not None and writer.write(block) != len(block):
                    raise AdoptionInvalid("INTERCHANGE_SHORT_WRITE")
            check_active()
        finally:
            close = getattr(iterator, "close", None)
            if callable(close):
                close()
        if count != item.bytes or digest.hexdigest() != value.sha256:
            raise AdoptionInvalid("INTERCHANGE_FILE_HASH")
        return count

    try:
        check_active()
        params_raw = read_json(params_ref, "input", 65536)
        params = InterchangeParseParams.model_validate_json(params_raw)
        if canonical_bytes(params) != params_raw:
            raise AdoptionInvalid("INTERCHANGE_PARAMS_INVALID")
        bundle = params.request.bundle
        if params.request.project_id.int == 0 or bundle.artifact_id.int == 0:
            raise AdoptionInvalid("INTERCHANGE_PARAMS_INVALID")
        source_ids = {params_ref.artifact_id, bundle.artifact_id}
        if root.artifact_id.int == 0 or root.artifact_id in source_ids:
            raise AdoptionInvalid("INTERCHANGE_OUTPUT_ALIAS")
        result_raw = read_json(root, "output", 8 << 20)
        result = ParsedInterchange.model_validate_json(result_raw)
        if canonical_bytes(result) != result_raw:
            raise AdoptionInvalid("INTERCHANGE_RESULT_INVALID")
        validate_parsed_inventory(result)
        archive = evidence(bundle, "input", "application/zip")
        limits = ArchiveLimits(archive.bytes, params.max_expanded_bytes, params.max_files)
        limits.validate()
        with scratch.reserve(archive.bytes) as reservation:
            path = reservation.path("archive", ".zip")
            with scratch.open_writer(path, reservation) as writer:
                stream(bundle, archive, limits.archive_bytes, writer)
            reservation.validate_file(path)
            with path.open("rb") as source:
                physical = verify_archive(source, limits, cancelled=lambda: (check_active(), False)[1])
                with zipfile.ZipFile(source) as zipped:
                    if params.request.format == "photoeditor_bundle":
                        metadata = next((item for item in physical if item.path == "manifest.json"), None)
                        if metadata is None or metadata.bytes > 8 << 20:
                            raise AdoptionInvalid("INTERCHANGE_MANIFEST_LIMIT")
                        with zipped.open("manifest.json") as content:
                            manifest_raw = content.read((8 << 20) + 1)
                        manifest = verify_bundle_manifest(manifest_raw, physical)
                    else:
                        sidecars = {item.path: item for item in physical
                            if PurePosixPath(item.path).suffix.lower() == '.xmp'}
                        class Sidecars(Mapping):
                            def __iter__(self):
                                return iter(sidecars)
                            def __len__(self):
                                return len(sidecars)
                            def __getitem__(self, path):
                                item = sidecars[path]
                                check_active()
                                if not 1 <= item.bytes <= 1 << 20:
                                    raise AdoptionInvalid('INTERCHANGE_XMP_LIMIT')
                                with zipped.open(path) as content:
                                    raw = content.read((1 << 20) + 1)
                                check_active()
                                return raw
                        manifest_raw = None
                        metadata = next((item for item in physical if item.path == 'manifest.json'), None)
                        if metadata is not None:
                            if not 2 <= metadata.bytes <= 8 << 20:
                                raise AdoptionInvalid('INTERCHANGE_MANIFEST_LIMIT')
                            check_active()
                            with zipped.open('manifest.json') as content:
                                manifest_raw = content.read((8 << 20) + 1)
                            check_active()
                        manifest = normalize_sidecar_inventory(bundle, physical, Sidecars(),
                            format=params.request.format, active=check_active, manifest_raw=manifest_raw).manifest
        if canonical_bytes(manifest) != canonical_bytes(result.manifest):
            raise AdoptionInvalid("INTERCHANGE_MANIFEST_SUBSTITUTED")
        inventory = sorted(manifest.inventory, key=lambda item: item.path)
        if len(result.files) != len(inventory):
            raise AdoptionInvalid("INTERCHANGE_OUTPUT_CLOSURE")
        observed = {item.path: item for item in physical}
        outputs, identifiers, total = {ref(root)}, {root.artifact_id}, len(result_raw)
        for expected, actual in zip(inventory, result.files):
            check_active()
            if (actual.path != expected.path or actual.contract != expected.contract
                    or actual.content.sha256 != expected.sha256 or actual.content.artifact_id.int == 0
                    or actual.content.artifact_id in source_ids | identifiers):
                raise AdoptionInvalid("INTERCHANGE_FILE_SUBSTITUTED")
            item = evidence(actual.content, "output", "application/octet-stream", "imported_file")
            if item.bytes != observed[expected.path].bytes:
                raise AdoptionInvalid("INTERCHANGE_FILE_SIZE")
            total += stream(actual.content, item, params.max_expanded_bytes)
            if total > params.max_expanded_bytes + (8 << 20):
                raise AdoptionInvalid("INTERCHANGE_OUTPUT_LIMIT")
            identifiers.add(actual.content.artifact_id)
            outputs.add(ref(actual.content))
        if reader.staged_outputs() != outputs:
            raise AdoptionInvalid("INTERCHANGE_OUTPUT_CLOSURE")
        check_active()
        return VerifiedParsedInterchange(params_raw, result_raw, total)
    except AdoptionInvalid:
        raise
    except ValueError as error:
        raise AdoptionInvalid("INTERCHANGE_PARSE_INVALID") from error


@dataclass(frozen=True)
class PreparedParsedAdoption:
    verified: VerifiedParsedInterchange
    adoption: PreparedAdoption
    ingest_items_jcs: bytes


def prepare_parsed_interchange_adoption(*, task_id, reader, params_ref, root,
                                       scratch, check_active=lambda: None):
    """Build scheduler inventory only after independent physical verification.

    Imported files are opaque bytes at parse adoption; archive contract labels
    cannot cause traversal as approved recipes/masks or grant any authority.
    Canonical ingest and typed remapping happen in the following import phase.
    """
    supplied_check_active = check_active
    def check_active():
        require_adoption_active(supplied_check_active)

    if task_id.int == 0:
        raise AdoptionInvalid("INTERCHANGE_TASK_INVALID")
    verified = verify_parsed_interchange(reader, params_ref, root, scratch,
                                        check_active=check_active)
    parsed = ParsedInterchange.model_validate_json(verified.result_jcs)
    references = [(root, "$")]
    references.extend((item.content, "$/files/" + str(index) + "/content")
                      for index, item in enumerate(parsed.files))
    if len(references) > 30001:
        raise AdoptionInvalid("INTERCHANGE_INVENTORY_LIMIT")
    inventory = []
    for ordinal, (reference, location) in enumerate(references):
        check_active()
        inventory.append({"artifact_id": str(reference.artifact_id),
                          "sha256": reference.sha256,
                          "purpose": sha256(("tasks/" + str(task_id) + "/" + location).encode()).hexdigest(),
                          "ordinal": ordinal})
    body = canonical_bytes(inventory)
    if len(body) > 16 << 20:
        raise AdoptionInvalid("INTERCHANGE_INVENTORY_LIMIT")
    from .interchange_ingest_snapshot import prepare_imported_ingest_snapshot
    ingest_items = prepare_imported_ingest_snapshot(verified.result_jcs, reader, check_active=check_active)
    return PreparedParsedAdoption(verified, PreparedAdoption(tuple(inventory), sha256(body).hexdigest()), ingest_items)
