"""Read-only scanner for legacy PhotoEditor directories.

The scanner never follows links and never trusts absolute paths from manifests.
It produces a deterministic plan that can be reviewed before an upload client is
allowed to create any platform resources.
"""
from __future__ import annotations

from dataclasses import asdict, dataclass
import hashlib
import json
from pathlib import Path, PurePosixPath
from typing import Any, Protocol
import urllib.error
import urllib.request
import uuid
import os
import time

MAX_JSON_BYTES = 32 * 1024 * 1024
MAX_FILES = 100_000


class MigrationError(ValueError):
    pass


class LegacyTransport(Protocol):
    def json(self, method:str, path:str, body:dict[str,Any]|None=None, *, idempotency_key:str|None=None)->dict[str,Any]:...
    def put(self, url:str, body:bytes)->str:...


class HTTPTransport:
    """Small owner-API transport. Bearer credentials are never serialized."""
    def __init__(self,base_url:str,token:str,timeout:float=30):
        if not base_url.startswith(('https://','http://')) or not token or not 1<=timeout<=120:raise MigrationError('invalid API transport')
        self.base_url=base_url.rstrip('/');self.token=token;self.timeout=timeout
    def json(self,method,path,body=None,*,idempotency_key=None):
        raw=None if body is None else _canonical(body);headers={'Authorization':'Bearer '+self.token,'Accept':'application/json'}
        if raw is not None:headers['Content-Type']='application/json'
        if idempotency_key is not None:headers['Idempotency-Key']=idempotency_key
        request=urllib.request.Request(self.base_url+path,data=raw,method=method,headers=headers)
        try:
            with urllib.request.urlopen(request,timeout=self.timeout) as response:
                payload=response.read(2*1024*1024+1)
        except urllib.error.HTTPError as exc:raise MigrationError(f'API {exc.code} for {path}') from exc
        except urllib.error.URLError as exc:raise MigrationError(f'API unavailable for {path}') from exc
        if len(payload)>2*1024*1024:raise MigrationError('API response too large')
        try:value=json.loads(payload,object_pairs_hook=_pairs,parse_constant=lambda x:(_ for _ in ()).throw(MigrationError('non-finite API JSON')))
        except (json.JSONDecodeError,UnicodeDecodeError) as exc:raise MigrationError('invalid API JSON') from exc
        if not isinstance(value,dict):raise MigrationError('API response must be an object')
        return value
    def put(self,url,body):
        if not url.startswith(('https://','http://')):raise MigrationError('invalid presigned URL')
        request=urllib.request.Request(url,data=body,method='PUT',headers={'Content-Length':str(len(body))})
        try:
            with urllib.request.urlopen(request,timeout=self.timeout) as response:etag=response.headers.get('ETag','').strip('"')
        except (urllib.error.HTTPError,urllib.error.URLError) as exc:raise MigrationError('part upload failed') from exc
        if not etag or len(etag)>256 or any(x in etag for x in '\r\n'):raise MigrationError('invalid upload ETag')
        return etag


def _pairs(values: list[tuple[str, Any]]) -> dict[str, Any]:
    out: dict[str, Any] = {}
    for key, value in values:
        if key in out:
            raise MigrationError(f"duplicate JSON key: {key}")
        out[key] = value
    return out


def _json(path: Path) -> dict[str, Any]:
    if path.is_symlink() or not path.is_file():
        raise MigrationError(f"manifest is not a regular file: {path.name}")
    raw = path.read_bytes()
    if len(raw) > MAX_JSON_BYTES:
        raise MigrationError(f"manifest exceeds {MAX_JSON_BYTES} bytes")
    try:
        value = json.loads(raw, object_pairs_hook=_pairs, parse_constant=lambda value: (_ for _ in ()).throw(MigrationError(f"non-finite JSON number: {value}")))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise MigrationError(f"invalid JSON: {path.name}") from exc
    if not isinstance(value, dict):
        raise MigrationError(f"manifest must be an object: {path.name}")
    return value


def _sha(path: Path) -> tuple[str, int]:
    digest, size = hashlib.sha256(), 0
    with path.open("rb") as stream:
        while block := stream.read(1024 * 1024):
            digest.update(block)
            size += len(block)
    return digest.hexdigest(), size


def _inside(root: Path, path: Path) -> Path:
    try:
        relative = path.relative_to(root)
    except ValueError as exc:
        raise MigrationError("manifest path escapes selected root") from exc
    current = root
    for part in relative.parts:
        current = current / part
        if current.is_symlink():
            raise MigrationError(f"symlink rejected: {relative.as_posix()}")
    return path


def _relative(raw: Any) -> PurePosixPath:
    if not isinstance(raw, str) or not raw or "\\" in raw or "\x00" in raw:
        raise MigrationError("invalid relative path")
    value = PurePosixPath(raw)
    if value.is_absolute() or any(part in ("", ".", "..") for part in value.parts):
        raise MigrationError("unsafe relative path")
    return value


@dataclass(frozen=True)
class ImportFile:
    kind: str
    relative: str
    sha256: str
    bytes: int
    run_id: str | None
    source_relative: str


@dataclass(frozen=True)
class ImportIssue:
    code: str
    path: str
    detail: str


@dataclass(frozen=True)
class LegacyImportPlan:
    schema_version: int
    root: str
    files: tuple[ImportFile, ...]
    runs: tuple[str, ...]
    provenance: tuple[dict[str, Any], ...]
    issues: tuple[ImportIssue, ...]
    plan_hash: str

    @property
    def valid(self) -> bool:
        return not any(issue.code in {"MISSING", "CORRUPT", "UNSAFE", "UNSUPPORTED_CONFIG"} for issue in self.issues)


def _canonical(value: Any) -> bytes:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


def scan(selected_root: Path) -> LegacyImportPlan:
    root = selected_root.expanduser().absolute()
    if root.is_symlink() or not root.is_dir():
        raise MigrationError("selected root must be a regular directory")
    _inside(root, root.resolve(strict=True))
    run_root = root / "output" / "runs"
    if not run_root.is_dir():
        run_root = root / "runs"
    manifests = sorted(run_root.glob("*/manifest.json")) if run_root.is_dir() else []
    if len(manifests) > MAX_FILES:
        raise MigrationError("too many legacy runs")
    files: dict[tuple[str, str, str | None], ImportFile] = {}
    issues: list[ImportIssue] = []
    provenance: list[dict[str, Any]] = []
    runs: list[str] = []
    seen_sources: dict[tuple[str, str], str] = {}
    for manifest_path in manifests:
        try:
            _inside(root, manifest_path.resolve(strict=True))
            manifest = _json(manifest_path)
            run_dir = manifest_path.parent
            run_id = run_dir.name
            if not run_id or run_id in runs:
                raise MigrationError("duplicate or empty run id")
            runs.append(run_id)
            raw_input = manifest.get("input_root")
            if not isinstance(raw_input, str):
                raise MigrationError("manifest input_root missing")
            input_root = _inside(root, Path(raw_input).expanduser().resolve(strict=True))
            items = manifest.get("items")
            if not isinstance(items, list) or not items:
                raise MigrationError("manifest items missing")
            report_path = run_dir / "report.json"
            report = _json(report_path)
            reports = report.get("images")
            if not isinstance(reports, list):
                raise MigrationError("report images missing")
            by_source = {item.get("source_id"): item for item in reports if isinstance(item, dict) and isinstance(item.get("source_id"), str)}
            config_path = run_dir / "resolved-config.json"
            config = _json(config_path)
            if config.get("schema_version") != 1:
                issues.append(ImportIssue("UNSUPPORTED_CONFIG", config_path.relative_to(root).as_posix(), "legacy config is preserved as provenance and cannot be converted to Style"))
            provenance.append({"run_id": run_id, "config": config, "conversion": "provenance_only"})
            for item in items:
                if not isinstance(item, dict):
                    raise MigrationError("manifest item must be an object")
                relative = _relative(item.get("relative"))
                expected = item.get("sha256")
                source_id = item.get("source_id")
                if not isinstance(expected, str) or len(expected) != 64 or not isinstance(source_id, str):
                    raise MigrationError("manifest item identity invalid")
                source = _inside(root, (input_root / Path(*relative.parts)).resolve(strict=False))
                source_name = source.relative_to(root).as_posix()
                if not source.is_file():
                    issues.append(ImportIssue("MISSING", source_name, "source file missing"))
                    continue
                actual, size = _sha(source)
                if actual != expected:
                    issues.append(ImportIssue("CORRUPT", source_name, "source SHA-256 differs from manifest"))
                    continue
                source_key = (relative.as_posix(), expected)
                if source_key in seen_sources:
                    issues.append(ImportIssue("DUPLICATE", source_name, f"also referenced by run {seen_sources[source_key]}"))
                else:
                    seen_sources[source_key] = run_id
                files.setdefault(("source", relative.as_posix(), None), ImportFile("source", source_name, expected, size, None, relative.as_posix()))
                record = by_source.get(source_id)
                if record is None:
                    issues.append(ImportIssue("UNLINKED", manifest_path.relative_to(root).as_posix(), f"source_id {source_id} absent from report"))
                    continue
                outputs = record.get("outputs")
                if not isinstance(outputs, dict):
                    issues.append(ImportIssue("UNLINKED", report_path.relative_to(root).as_posix(), f"source_id {source_id} has no outputs"))
                    continue
                for raw_output, output_hash in sorted(outputs.items()):
                    output_rel = _relative(raw_output)
                    if not isinstance(output_hash, str) or len(output_hash) != 64:
                        raise MigrationError("output digest invalid")
                    output = _inside(root, (run_dir / Path(*output_rel.parts)).resolve(strict=False))
                    output_name = output.relative_to(root).as_posix()
                    if not output.is_file():
                        issues.append(ImportIssue("MISSING", output_name, "declared output missing"))
                        continue
                    actual, size = _sha(output)
                    if actual != output_hash:
                        issues.append(ImportIssue("CORRUPT", output_name, "output SHA-256 differs from report"))
                        continue
                    files[("output", output_name, run_id)] = ImportFile("output", output_name, actual, size, run_id, relative.as_posix())
        except (MigrationError, OSError) as exc:
            name = manifest_path.relative_to(root).as_posix() if manifest_path.is_relative_to(root) else manifest_path.name
            issues.append(ImportIssue("UNSAFE", name, str(exc)))
    payload = {"schema_version": 1, "root": root.as_posix(), "files": [asdict(value) for value in sorted(files.values(), key=lambda value: (value.kind, value.run_id or "", value.relative))], "runs": sorted(runs), "provenance": sorted(provenance, key=lambda value: value["run_id"]), "issues": [asdict(value) for value in sorted(issues, key=lambda value: (value.code, value.path, value.detail))]}
    plan_hash = hashlib.sha256(_canonical(payload)).hexdigest()
    return LegacyImportPlan(files=tuple(ImportFile(**value) for value in payload["files"]), runs=tuple(payload["runs"]), provenance=tuple(payload["provenance"]), issues=tuple(ImportIssue(**value) for value in payload["issues"]), plan_hash=plan_hash, schema_version=1, root=root.as_posix())


def validate(plan: LegacyImportPlan) -> tuple[ImportIssue, ...]:
    """Rehash a plan without writing; changed files make execution ineligible."""
    root = Path(plan.root)
    issues = list(plan.issues)
    for item in plan.files:
        try:
            path = _inside(root, (root / item.relative).resolve(strict=False))
            if not path.is_file():
                issues.append(ImportIssue("MISSING", item.relative, "file disappeared after scan"))
                continue
            digest, size = _sha(path)
            if digest != item.sha256 or size != item.bytes:
                issues.append(ImportIssue("CORRUPT", item.relative, "file changed after scan"))
        except (MigrationError, OSError) as exc:
            issues.append(ImportIssue("UNSAFE", item.relative, str(exc)))
    return tuple(sorted(issues, key=lambda value: (value.code, value.path, value.detail)))


def plan_document(plan: LegacyImportPlan) -> dict[str, Any]:
    return {"schema_version": plan.schema_version, "root": plan.root, "files": [asdict(value) for value in plan.files], "runs": list(plan.runs), "provenance": list(plan.provenance), "issues": [asdict(value) for value in plan.issues], "plan_hash": plan.plan_hash, "valid": plan.valid}


def _upload_key(plan_hash:str,item:ImportFile,stage:str)->str:
    return 'legacy-'+hashlib.sha256(_canonical({'plan':plan_hash,'file':item.relative,'sha256':item.sha256,'stage':stage})).hexdigest()


def _collection_key(plan_hash: str, run_id: str | None) -> str:
    return "legacy-" + hashlib.sha256(_canonical({"plan": plan_hash, "collection": run_id or "sources"})).hexdigest()


def _collection_name(plan_hash: str, run_id: str | None) -> str:
    label = "originals" if run_id is None else "run " + run_id
    label = "".join(character if character >= " " and character != "\x7f" else "_" for character in label).strip()
    return ("Legacy " + label)[:200] or ("Legacy import " + plan_hash[:12])


def create_collection(plan: LegacyImportPlan, transport: LegacyTransport, tenant_id: str, project_id: str, run_id: str | None) -> str:
    response = transport.json(
        "POST",
        f"/api/v1/tenants/{tenant_id}/collections",
        {
            "project_id": project_id,
            "name": _collection_name(plan.plan_hash, run_id),
            "description": f"Legacy import {plan.plan_hash}; " + ("original sources" if run_id is None else f"run {run_id}"),
        },
        idempotency_key=_collection_key(plan.plan_hash, run_id),
    )
    collection = response.get("collection")
    identifier = collection.get("id") if isinstance(collection, dict) else None
    try:
        uuid.UUID(identifier)
    except (TypeError, ValueError) as exc:
        raise MigrationError("invalid collection response") from exc
    return identifier


def upload_file(plan:LegacyImportPlan,item:ImportFile,transport:LegacyTransport,tenant_id:str,project_id:str,collection_id:str|None=None)->dict[str,Any]:
    """Upload one immutable plan item through the ordinary resumable owner API."""
    try:uuid.UUID(tenant_id);uuid.UUID(project_id)
    except ValueError as exc:raise MigrationError('invalid tenant or project id') from exc
    if collection_id is not None:
        try:uuid.UUID(collection_id)
        except ValueError as exc:raise MigrationError('invalid collection id') from exc
    root=Path(plan.root);path=_inside(root,(root/item.relative).resolve(strict=False));digest,size=_sha(path)
    if digest!=item.sha256 or size!=item.bytes:raise MigrationError('file changed after scan')
    suffix=path.suffix.lower().lstrip('.');fmt={'jpeg':'jpeg','jpg':'jpeg','tiff':'tiff','tif':'tiff'}.get(suffix,suffix)
    if fmt not in {'jpeg','png','tiff','dng','cr2','nef','arw','raf','orf','rw2'}:raise MigrationError('unsupported upload format')
    begin={'project_id':project_id,'filename':path.name,'bytes':size,'format':fmt,'sha256':digest}
    if collection_id is not None:begin['collection_id']=collection_id
    base=f'/api/v1/tenants/{tenant_id}/uploads';session=transport.json('POST',base,begin,idempotency_key=_upload_key(plan.plan_hash,item,'begin'))
    upload_id=session.get('id');state=session.get('state');part_bytes=session.get('part_bytes');parts_total=session.get('parts_total')
    try:uuid.UUID(upload_id)
    except (ValueError,TypeError) as exc:raise MigrationError('invalid upload session id') from exc
    if state in ('completing','completed','verifying','verified'):
        return {**session,'upload_id':upload_id}
    if state!='uploading' or type(part_bytes) is not int or type(parts_total) is not int or part_bytes<1 or not 1<=parts_total<=10000 or parts_total!=(size+part_bytes-1)//part_bytes:raise MigrationError('invalid upload session')
    numbers=list(range(1,parts_total+1));urls={}
    for offset in range(0,len(numbers),100):
        signed=transport.json('POST',f'{base}/{upload_id}/parts',{'part_numbers':numbers[offset:offset+100]})
        for entry in signed.get('items',[]):
            if not isinstance(entry,dict) or type(entry.get('part_number')) is not int or not isinstance(entry.get('url'),str):raise MigrationError('invalid signed part response')
            if entry['part_number'] in urls:raise MigrationError('duplicate signed part')
            urls[entry['part_number']]=entry['url']
    if sorted(urls)!=numbers:raise MigrationError('incomplete signed part response')
    parts=[]
    with path.open('rb') as source:
        for number in numbers:
            body=source.read(part_bytes)
            if not body or number<parts_total and len(body)!=part_bytes:raise MigrationError('unexpected upload part layout')
            parts.append({'part_number':number,'etag':transport.put(urls[number],body)})
        if source.read(1):raise MigrationError('upload size exceeds plan')
    completed=transport.json('POST',f'{base}/{upload_id}/complete',{'parts':parts,'expected_size':size},idempotency_key=_upload_key(plan.plan_hash,item,'complete'))
    return {**completed,'upload_id':upload_id}


def reconcile_file(plan:LegacyImportPlan,item:ImportFile,upload:dict[str,Any],transport:LegacyTransport,tenant_id:str,*,deadline:float,poll_seconds:float=2)->dict[str,Any]:
    upload_id=upload.get('upload_id')
    try:uuid.UUID(upload_id)
    except (TypeError,ValueError) as exc:raise MigrationError('upload response has no valid upload id') from exc
    path=f'/api/v1/tenants/{tenant_id}/uploads/{upload_id}'
    while True:
        status=transport.json('GET',path)
        state=status.get('state')
        if state=='verified':break
        if state in {'rejected','failed','aborted','expired'}:raise MigrationError(f'upload {upload_id} ended in {state}')
        if state not in {'initializing','uploading','completing','verifying'}:raise MigrationError('invalid upload status')
        remaining=deadline-time.monotonic()
        if remaining<=0:raise MigrationError(f'upload {upload_id} verification timed out; rerun with the same plan')
        time.sleep(min(poll_seconds,remaining))
    result=transport.json('POST',f'/api/v1/tenants/{tenant_id}/legacy-imports/reconcile',{
        'plan_hash':plan.plan_hash,'source_relative':item.relative,'source_sha256':item.sha256,'upload_id':upload_id,
        'provenance':{'kind':item.kind,'run_id':item.run_id,'source_relative':item.source_relative},
    })
    if not isinstance(result,dict) or set(result)!={'id','plan_hash','source_relative','source_sha256','asset_version_id','upload_id','provenance'}:
        raise MigrationError('invalid legacy reconciliation receipt')
    for field in ('id','asset_version_id','upload_id'):
        try:
            value=uuid.UUID(result[field])
            if value.int==0 or str(value)!=result[field]:raise ValueError('noncanonical receipt identity')
        except (ValueError,TypeError,AttributeError) as exc:
            raise MigrationError('invalid legacy reconciliation identity') from exc
    if result['upload_id']!=upload_id or result['plan_hash']!=plan.plan_hash or result['source_relative']!=item.relative or result['source_sha256']!=item.sha256 or not isinstance(result['provenance'],dict):
        raise MigrationError('foreign legacy reconciliation receipt')
    return json.loads(json.dumps(result))


def execute(plan:LegacyImportPlan,transport:LegacyTransport,tenant_id:str,project_id:str,collection_id:str|None=None,*,wait_seconds:float=3600)->tuple[dict[str,Any],...]:
    issues=validate(plan)
    if any(x.code in {'MISSING','CORRUPT','UNSAFE','UNSUPPORTED_CONFIG'} for x in issues):raise MigrationError('legacy plan is not executable')
    try:uuid.UUID(tenant_id);uuid.UUID(project_id)
    except ValueError as exc:raise MigrationError('invalid tenant or project id') from exc
    if collection_id is not None:
        try:uuid.UUID(collection_id)
        except ValueError as exc:raise MigrationError('invalid collection id') from exc
    if not 0<wait_seconds<=86400:raise MigrationError('invalid verification timeout')
    collections:dict[str|None,str]={None:collection_id or create_collection(plan,transport,tenant_id,project_id,None)}
    for run_id in plan.runs:
        collections[run_id]=create_collection(plan,transport,tenant_id,project_id,run_id)
    uploads=tuple(upload_file(plan,item,transport,tenant_id,project_id,collections[item.run_id]) for item in plan.files)
    deadline=time.monotonic()+wait_seconds
    return tuple(reconcile_file(plan,item,upload,transport,tenant_id,deadline=deadline) for item,upload in zip(plan.files,uploads,strict=True))


def main(argv: list[str] | None = None) -> int:
    import argparse
    parser = argparse.ArgumentParser(description="Read-only PhotoEditor legacy import planner")
    parser.add_argument("root", type=Path, help="Selected legacy directory; symlinks and paths outside it are rejected")
    parser.add_argument("--output", type=Path, help="Optional JSON plan path inside the selected root")
    parser.add_argument("--execute-api", help="Explicit owner API base URL; requires PHOTOEDITOR_TOKEN")
    parser.add_argument("--tenant-id")
    parser.add_argument("--project-id")
    parser.add_argument("--collection-id")
    parser.add_argument("--wait-seconds",type=float,default=3600,help="Maximum total wait for ingest verification (1..86400)")
    args = parser.parse_args(argv)
    try:
        plan = scan(args.root)
        encoded = json.dumps(plan_document(plan), ensure_ascii=False, sort_keys=True, indent=2) + "\n"
        if args.output is None:
            print(encoded, end="")
        else:
            root = Path(plan.root)
            output = _inside(root, args.output.expanduser().absolute())
            output.parent.mkdir(parents=True, exist_ok=True)
            temporary = output.with_name("." + output.name + ".tmp")
            temporary.write_text(encoded)
            temporary.replace(output)
        if args.execute_api:
            token=os.environ.get('PHOTOEDITOR_TOKEN','')
            if not plan.valid or not token or not args.tenant_id or not args.project_id:raise MigrationError('execution requires a valid plan, tenant/project IDs and PHOTOEDITOR_TOKEN')
            results=execute(plan,HTTPTransport(args.execute_api,token),args.tenant_id,args.project_id,args.collection_id,wait_seconds=args.wait_seconds)
            print(json.dumps({'plan_hash':plan.plan_hash,'reconciled':len(results),'results':results},ensure_ascii=False,sort_keys=True))
        elif any((args.tenant_id,args.project_id,args.collection_id)):
            raise MigrationError('tenant/project/collection options require --execute-api')
        return 0 if plan.valid else 2
    except (MigrationError, OSError) as exc:
        print(json.dumps({"code": "LEGACY_SCAN_FAILED", "detail": str(exc)}, ensure_ascii=False))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
