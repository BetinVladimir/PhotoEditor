"""Bounded dependency-first remapping; only explicitly typed documents qualify."""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID, uuid5

from photo_worker.contracts.base import canonical_bytes, parse_json
from .interchange_parse import ParsedInterchange


class UnsupportedDependency(ValueError):
    pass


@dataclass(frozen=True)
class RemappedDocument:
    original_id: UUID
    artifact_id: UUID
    sha256: str
    contract: str
    body: bytes


# These authorities are never transported by an imported recipe graph.
_FORBIDDEN_CONTRACTS = frozenset({
    "ConsentSnapshot", "SigningPolicy", "SigningLineage", "TrainingManifest",
    "LearningModel", "LearningVersion", "CandidateProfile", "ApprovedSemanticEdit",
})
_FORBIDDEN_FIELDS = frozenset({
    "consent", "consents", "approval", "approvals", "approved_by", "approved_at",
    "signing_policy", "private_key", "auth_session_id", "tenant_id", "project_id",
})


class BundleRemapper:
    """Caller supplies server allowlists and authoritative canonical identities.

    read_json consumes already physically verified imported-file refs. validators
    map an explicit contract to a strict typed validator returning JSON/model.
    Binary contracts need independent ingest/type qualification before allowlist.
    This component does not publish documents or register permissions/approvals.
    """
    def __init__(self, parsed: ParsedInterchange, tenant: UUID, project: UUID, job: UUID,
                 read_json, validators, binary_contracts, *, canonical_ids=None,
                 max_documents=4096, max_json_bytes=32 << 20, check_active=lambda: None,
                 binding_rewriter=None, binary_qualifier=None):
        if (not isinstance(parsed, ParsedInterchange) or any(value.int == 0 for value in (tenant, project, job))
                or not callable(read_json) or not callable(check_active)
                or type(max_documents) is not int or not 1 <= max_documents <= 30000
                or type(max_json_bytes) is not int or not 1 <= max_json_bytes <= 128 << 20):
            raise ValueError("INTERCHANGE_REMAP_CONFIGURATION")
        self.namespace = uuid5(tenant, "interchange:" + str(project) + ":" + str(job))
        self.read_json, self.check_active = read_json, check_active
        if any(port is not None and not callable(port) for port in (binding_rewriter,binary_qualifier)):
            raise ValueError("INTERCHANGE_REMAP_QUALIFICATION")
        self.binding_rewriter, self.binary_qualifier = binding_rewriter, binary_qualifier
        self.validators = dict(validators)
        self.binary_contracts = frozenset(binary_contracts)
        if (self.binary_contracts & self.validators.keys()
                or (_FORBIDDEN_CONTRACTS & (self.binary_contracts | self.validators.keys()))
                or any(not callable(value) for value in self.validators.values())):
            raise ValueError("INTERCHANGE_REMAP_CONTRACTS")
        self.canonical_ids = dict(canonical_ids or {})
        if any(not isinstance(a, UUID) or not isinstance(b, UUID) or a.int == 0 or b.int == 0
               for a, b in self.canonical_ids.items()):
            raise ValueError("INTERCHANGE_REMAP_IDENTITIES")
        self.max_documents, self.max_json_bytes = max_documents, max_json_bytes
        self.bytes_read, self.visiting, self.finished, self.documents = 0, set(), {}, {}
        files = {item.path: item for item in parsed.files}
        if len(files) != len(parsed.files) or len(files) != len(parsed.manifest.inventory):
            raise ValueError("INTERCHANGE_REMAP_INVENTORY")
        self.inventory = {}
        output_ids = set()
        for item in parsed.manifest.inventory:
            found = files.get(item.path)
            if (found is None or found.contract != item.contract or found.content.sha256 != item.sha256
                    or item.artifact_id.int == 0 or item.artifact_id in self.inventory
                    or found.content.artifact_id.int == 0 or found.content.artifact_id in output_ids):
                raise ValueError("INTERCHANGE_REMAP_INVENTORY")
            output_ids.add(found.content.artifact_id)
            self.inventory[item.artifact_id] = (item, found.content)

    def identity(self, original):
        return self.canonical_ids.get(original, uuid5(self.namespace, "entity:" + str(original)))

    def remap(self, original: UUID, expected_sha=None, *, _depth=0):
        self.check_active()
        if _depth > 32 or original not in self.inventory:
            raise UnsupportedDependency("INTERCHANGE_DEPENDENCY_MISSING")
        item, content = self.inventory[original]
        if expected_sha is not None and expected_sha != item.sha256:
            raise UnsupportedDependency("INTERCHANGE_DEPENDENCY_HASH")
        if original in self.visiting:
            raise UnsupportedDependency("INTERCHANGE_DEPENDENCY_CYCLE")
        if original in self.finished:
            return dict(self.finished[original])
        if item.contract in self.binary_contracts:
            if self.binary_qualifier is not None:
                self.binary_qualifier(item.contract, content.model_copy(deep=True))
                self.check_active()
            result = {"artifact_id": str(content.artifact_id), "sha256": content.sha256}
            self.finished[original] = result
            return dict(result)
        validator = self.validators.get(item.contract)
        if validator is None or item.contract in _FORBIDDEN_CONTRACTS:
            raise UnsupportedDependency("INTERCHANGE_DEPENDENCY_UNSUPPORTED")
        if len(self.documents) + len(self.visiting) >= self.max_documents:
            raise UnsupportedDependency("INTERCHANGE_DOCUMENT_LIMIT")
        raw = self.read_json(content, min(8 << 20, self.max_json_bytes - self.bytes_read))
        if not isinstance(raw, bytes) or not 1 <= len(raw) <= min(8 << 20, self.max_json_bytes - self.bytes_read) or sha256(raw).hexdigest() != item.sha256:
            raise UnsupportedDependency("INTERCHANGE_DOCUMENT_HASH")
        self.bytes_read += len(raw)
        value = parse_json(raw, max_bytes=8 << 20)
        # Typed original validation precedes remap; validators cannot drop fields.
        if canonical_bytes(validator(value)) != canonical_bytes(value):
            raise UnsupportedDependency("INTERCHANGE_DOCUMENT_FIELDS")
        self.visiting.add(original)
        try:
            mapped = self._walk(value, _depth + 1)
            if canonical_bytes(validator(mapped)) != canonical_bytes(mapped):
                raise UnsupportedDependency("INTERCHANGE_REMAPPED_FIELDS")
            body = canonical_bytes(mapped)
            identifier = uuid5(self.namespace, "document:" + str(original))
            document = RemappedDocument(original, identifier, sha256(body).hexdigest(), item.contract, body)
            self.documents[original] = document
            result = {"artifact_id": str(identifier), "sha256": document.sha256}
            self.finished[original] = result
            return dict(result)
        finally:
            self.visiting.remove(original)

    def _walk(self, value, depth):
        if depth > 32:
            raise UnsupportedDependency("INTERCHANGE_JSON_DEPTH")
        if isinstance(value, dict):
            if set(value) == {"asset_version_id", "source", "canvas_hash"} and self.binding_rewriter is not None:
                from photo_worker.operations.session.contracts import VersionBinding
                original = VersionBinding.model_validate_json(canonical_bytes(value))
                mapped = self.binding_rewriter(original.model_copy(deep=True))
                if not isinstance(mapped,VersionBinding):
                    raise UnsupportedDependency("INTERCHANGE_BINDING_UNQUALIFIED")
                return mapped.model_dump(mode="json")
            if set(value) == {"artifact_id", "sha256"}:
                return self.remap(UUID(value["artifact_id"]), value["sha256"], _depth=depth)
            if any(key in _FORBIDDEN_FIELDS and child is not None for key, child in value.items()):
                raise UnsupportedDependency("INTERCHANGE_FOREIGN_AUTHORITY")
            if value.get("learning_version_id") is not None:
                raise UnsupportedDependency("INTERCHANGE_FOREIGN_LEARNING")
            return {key: self._walk(child, depth + 1) for key, child in value.items()}
        if isinstance(value, list):
            return [self._walk(child, depth + 1) for child in value]
        if isinstance(value, str):
            try:
                identifier = UUID(value)
            except ValueError:
                return value
            if str(identifier) == value:
                if identifier.int == 0:
                    raise UnsupportedDependency("INTERCHANGE_ZERO_IDENTITY")
                return str(self.identity(identifier))
        return value
