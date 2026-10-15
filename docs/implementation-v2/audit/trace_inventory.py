"""Inventory current V20–V32 trace candidates without claiming acceptance.

Run from any directory. Exact operation literals and handler identifiers are
search hints only; runtime registration, authority and physical execution need
independent review. This script never reads historical PASS as current proof.
"""
import hashlib
import json
import re
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
CONTRACTS = ROOT / "docs/implementation-v2/contracts"
DEST = Path(__file__).resolve().parent
EXCLUDED = {"node_modules", "__pycache__", ".git", ".next", ".expo", "dist", "build"}
STAGES = {
    "api": ("backend/internal/httpapi", {".go"}),
    "service": ("backend/internal", {".go"}),
    "sql": ("backend/db/migrations", {".sql"}),
    "worker": ("workers/photo_worker", {".py"}),
    "client": ("apps/photographer/src", {".ts", ".tsx"}),
    "platform_tests": ("tests", {".py"}),
}


def relative(path):
    return str(path.relative_to(ROOT))


def source_files(base, suffixes):
    return sorted(
        path for path in (ROOT / base).rglob("*")
        if path.is_file() and path.suffix in suffixes
        and not EXCLUDED.intersection(path.parts)
    )


def is_test(path):
    name = path.name
    return name.startswith("test_") or "_test." in name or ".test." in name or ".spec." in name


def inventory():
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    operation_contract = CONTRACTS / "operations.json"
    endpoint_contract = CONTRACTS / "endpoints.json"
    operations = json.loads(operation_contract.read_text())["operations"]
    endpoints = json.loads(endpoint_contract.read_text())["endpoints"]
    indexed = {}
    for stage, (base, suffixes) in STAGES.items():
        indexed[stage] = [
            (relative(path), path.read_text(errors="replace"))
            for path in source_files(base, suffixes)
            if stage == "platform_tests" or not is_test(path)
        ]
    unit_files = []
    for base, suffixes in (
        ("backend/internal", {".go"}),
        ("workers", {".py"}),
        ("apps/photographer/src", {".ts", ".tsx"}),
    ):
        unit_files.extend(
            (relative(path), path.read_text(errors="replace"))
            for path in source_files(base, suffixes) if is_test(path)
        )

    def matches(pattern, source):
        paths = [path for path, body in source if pattern.search(body)]
        return {"count": len(paths), "files": paths[:12], "truncated": len(paths) > 12}

    mounted_routes = {}
    for path, body in indexed["api"]:
        for method, route in re.findall(r'\.(Get|Post|Put|Patch|Delete)\s*\(\s*"([^"]+)"', body):
            mounted_routes.setdefault((method.upper(), route), set()).add(path)

    operation_rows = []
    for operation in operations:
        identifier = operation["id"]
        literal = re.compile(r"(?<![A-Za-z0-9_.])['\"]" + re.escape(identifier) + r"['\"]")
        row = {"id": identifier, "package": operation["package"], "version": operation["version"]}
        for stage, source in indexed.items():
            row[stage + "_candidates"] = matches(literal, source)
        row["unit_candidates"] = matches(literal, unit_files)
        row["trace_status"] = "not_verified"
        row["review_gate"] = "request → authorization/frozen inputs → admission/dispatch → worker → result/adoption → UI decision → retry/cancel → GC/purge"
        operation_rows.append(row)

    endpoint_rows = []
    for endpoint in endpoints:
        handler = endpoint["handler"]
        symbol = re.compile(r"\b" + re.escape(handler) + r"\b")
        route_files = sorted(mounted_routes.get((endpoint["method"], endpoint["path"]), set()))
        dynamic_route_files = sorted(
            path for path, body in indexed["api"]
            if endpoint["method"] == "GET"
            and re.search(r'\{\s*"' + re.escape(endpoint["path"]) + r'"\s*,', body)
            and re.search(r'\.Get\s*\(\s*listing\.path\s*,', body)
        ) if not route_files else []
        endpoint_rows.append({
            "method": endpoint["method"], "path": endpoint["path"],
            "package": endpoint["package"], "handler": handler,
            "api_handler_candidates": matches(symbol, indexed["api"]),
            "mounted_route_candidates": {"count": len(route_files), "files": route_files},
            "dynamic_route_candidates": {"count": len(dynamic_route_files), "files": dynamic_route_files},
            "service_candidates": matches(symbol, indexed["service"]),
            "unit_candidates": matches(symbol, unit_files),
            "route_mount_status": ("static_declaration_only" if route_files else
                                   "static_dynamic_table_candidate" if dynamic_route_files else
                                   "not_found_by_static_scan"),
            "trace_status": "not_verified",
            "review_gate": "mounted route → auth/tenant scope → service/SQL → response/error → client consumer → idempotency/revocation",
        })
    return {
        "schema_version": 1,
        "source_commit": commit,
        "scope": "current source candidates for all V20–V32 operations and endpoint contracts",
        "contract_sha256": {
            relative(path): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in (operation_contract, endpoint_contract)
        },
        "limitations": [
            "Literal/symbol matches are discovery hints, not runtime installation or acceptance evidence.",
            "Nested API route prefixes and client navigation are not resolved by this scan.",
            "SQL/worker matches do not prove authorization, qualification, result adoption, GC or purge.",
            "Historical tests are not evidence for the current build; all rows remain not_verified.",
        ],
        "operations": operation_rows, "endpoints": endpoint_rows,
    }


def main():
    report = inventory()
    (DEST / "trace-inventory.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    no_worker = [row["id"] for row in report["operations"] if row["worker_candidates"]["count"] == 0]
    no_api_handler = [f'{row["method"]} {row["path"]}' for row in report["endpoints"]
                      if row["api_handler_candidates"]["count"] == 0]
    no_route = [f'{row["method"]} {row["path"]}' for row in report["endpoints"]
                if row["mounted_route_candidates"]["count"] == 0
                and row["dynamic_route_candidates"]["count"] == 0]
    lines = [
        "# Текущий реестр кандидатов трасс V20–V32", "",
        "Машинный реестр: [trace-inventory.json](trace-inventory.json). Он перечисляет все операции и API-контракты, но ни одна строка не считается принятой по статическому совпадению.", "",
        f'Операции: {len(report["operations"])}; endpoint-контракты: {len(report["endpoints"])}. Кандидатов worker без точного литерала: {len(no_worker)}; endpoint без статического route-кандидата: {len(no_route)}; без точного имени handler в API: {len(no_api_handler)} (включая inline handlers).', "",
        "## Следующие ручные проверки", "",
        "Для каждой строки требуется проследить mounted route и клиент, полномочия и frozen входы, admission/dispatch, worker result, transactional adoption, review/download, отказ/повтор и физический GC/purge. Отсутствие точного литерала может означать косвенную регистрацию; наличие литерала не доказывает работу.", "",
        "### Операции без точного worker-кандидата", "",
    ]
    lines.extend(f"- `{name}`" for name in no_worker)
    if not no_worker:
        lines.append("- Нет по этому статическому признаку.")
    lines.extend(["", "### Endpoint-контракты без статического route-кандидата", ""])
    lines.extend(f"- `{name}`" for name in no_route)
    if not no_route:
        lines.append("- Нет по этому статическому признаку.")
    lines.extend(["", "### Endpoint-контракты без точного API handler-кандидата", ""])
    lines.extend(f"- `{name}`" for name in no_api_handler)
    lines.extend(["", "Этот список не означает отсутствующий endpoint: inline-функции и динамические таблицы маршрутов не содержат нормативное имя handler. Каждое совпадение и статус доступны в JSON; дальнейшая проверка должна проставить доказательство по слоям без переноса исторических PASS.", ""])
    (DEST / "trace-inventory.md").write_text("\n".join(lines))
    print(json.dumps({"operations": len(report["operations"]), "endpoints": len(report["endpoints"]),
                      "no_worker_literal": len(no_worker), "no_static_route_candidate": len(no_route),
                      "no_api_handler_symbol": len(no_api_handler)}))


if __name__ == "__main__":
    main()
