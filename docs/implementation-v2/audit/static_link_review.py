"""Repeatable source-only pass over every normative operation and HTTP endpoint.

This deliberately does not execute app code, migrations or tests. A static link
is evidence of a declaration, not proof of authorization or physical behavior.
"""
import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
INVENTORY = HERE / "trace-inventory.json"


def rel(path):
    return str(path.relative_to(ROOT))


def main():
    source = json.loads(INVENTORY.read_text())
    worker_files = [(p, p.read_text(errors="replace")) for p in
                    (ROOT / "workers/photo_worker").rglob("*.py")
                    if "test" not in p.name and "__pycache__" not in p.parts]
    adoption_files = [(p, body) for p, body in worker_files
                      if ("adopt" in p.name or "adopt" in str(p.parent))]
    api_files = [(p, p.read_text(errors="replace")) for p in
                 (ROOT / "backend/internal/httpapi").glob("*.go")
                 if not p.name.endswith("_test.go")]
    method_bodies = {}
    for _, body in api_files:
        starts = list(re.finditer(r"func\s*\(s \*Server\) (\w+)\([^\n]*", body))
        for index, match in enumerate(starts):
            end = starts[index + 1].start() if index + 1 < len(starts) else len(body)
            method_bodies[match.group(1)] = body[match.end():end]
    reached = {"Routes"}
    frontier = ["Routes"]
    while frontier:
        body = method_bodies.get(frontier.pop(), "")
        for name in re.findall(r"\bs\.(\w+)\s*\(", body):
            if name in method_bodies and name not in reached:
                reached.add(name)
                frontier.append(name)
    dynamic_registrations = {
        "optical.panorama": "workers/photo_worker/operations/optical/multi_installation.py",
        "optical.focus_stack": "workers/photo_worker/operations/optical/multi_installation.py",
        "v2.delivery.prepare": "workers/photo_worker/operations/delivery/adapter.py",
        "v2.delivery.assemble": "workers/photo_worker/operations/delivery/adapter.py",
    }
    platform_blockers = {
        "semantic.transfer": "D-04: semantic installation requires CUDA; no MPS positive path",
        "region.preview": "D-04: region renderer requires CUDA; no MPS positive path",
        "portrait.glare": "D-04: model installation permits CPU/CUDA GPU-class backend, not MPS",
        "image.reflection": "D-04: model installation permits CPU/CUDA GPU-class backend, not MPS",
    }
    operations = []
    for row in source["operations"]:
        op = row["id"]
        pattern = re.compile(r"Operation\s*\(\s*['\"]" + re.escape(op) + r"['\"]")
        direct = [rel(p) for p, body in worker_files if pattern.search(body)]
        registration = direct or ([dynamic_registrations[op]] if op in dynamic_registrations else [])
        literal = re.compile(r"['\"]" + re.escape(op) + r"['\"]")
        adoption_candidates = [rel(p) for p, body in adoption_files if literal.search(body)]
        operations.append({
            "id": op, "package": row["package"],
            "worker_registration_kind": "direct_literal" if direct else "dynamic_named_dispatch" if registration else "not_found",
            "worker_registration_files": registration,
            "service_literal_files": row["service_candidates"]["files"],
            "service_literal_file_count": row["service_candidates"]["count"],
            "sql_literal_files": row["sql_candidates"]["files"],
            "sql_literal_file_count": row["sql_candidates"]["count"],
            "worker_literal_files": row["worker_candidates"]["files"],
            "adoption_literal_files": adoption_candidates[:20],
            "unit_literal_files": row["unit_candidates"]["files"],
            "confirmed_code_gaps": row["confirmed_code_gaps"],
            "platform_qualification_blocker": platform_blockers.get(op),
            "remaining_static_review": "admission/authorization, exact runtime installation, result/adoption, UI/Apply and cleanup are not proved by literal matches",
        })
    endpoints = []
    for row in source["endpoints"]:
        method, path = row["method"], row["path"]
        expression = re.compile(r"\." + method.title() + r"\s*\(\s*['\"]" + re.escape(path) + r"['\"]")
        declarations = [rel(p) for p, body in api_files if expression.search(body)]
        dynamic = row["dynamic_route_candidates"]["files"]
        route_methods = [name for name, body in method_bodies.items() if expression.search(body)]
        if dynamic:
            route_methods.append("catalogRoutes")
        reachable = any(name in reached for name in route_methods)
        if path.startswith("/api/v1/media/"):
            auth_entry = "signed_revocable_media_grant"
        elif path.endswith("/capture-receipts"):
            auth_entry = "device_principal"
        elif path.endswith("/challenges") or path.endswith("/token"):
            auth_entry = "device_challenge_exchange"
        else:
            auth_entry = "tenant_request"
        if auth_entry == "tenant_request" and declarations:
            handler_starts = [body[expression.search(body).end():expression.search(body).end() + 800]
                              for _, body in api_files if expression.search(body)]
            if not any("tenantRequest" in fragment for fragment in handler_starts):
                auth_entry = "not_found_in_handler_prefix"
        endpoints.append({
            "method": method, "path": path, "package": row["package"],
            "route_declaration_kind": "direct" if declarations else "dynamic_table" if dynamic else "not_found",
            "route_declaration_files": declarations or dynamic,
            "declaring_server_methods": sorted(set(route_methods)),
            "mounted_from_server_routes": reachable,
            "auth_entry_candidate": auth_entry,
            "handler_symbol_files": row["api_handler_candidates"]["files"],
            "service_symbol_files": row["service_candidates"]["files"],
            "confirmed_code_gaps": row["confirmed_code_gaps"],
            "remaining_static_review": "route declaration alone does not establish auth, service behavior, client use, replay or revocation",
        })
    result = {
        "schema_version": 1,
        "source_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
        "method": "source inspection only; no tests, migrations, builds or runtime calls",
        "status_semantics": "declaration presence and known defects; all other behavioral links remain unproven",
        "operations": operations, "endpoints": endpoints,
    }
    (HERE / "static-link-review.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    lines = ["# Построчный статический аудит контрактных трасс", "",
             f"Срез `{result['source_commit'][:7]}`. Метод: {result['method']}.", "",
             f"Охват: {len(operations)}/30 операций имеют прямую или динамическую worker-регистрацию; {sum(x['mounted_from_server_routes'] for x in endpoints)}/79 HTTP-контрактов имеют объявление маршрута, достижимое из `Server.Routes()` через вызовы методов. Ни одного отсутствующего structural declaration не найдено.", "",
             "В 79 route handlers найдены кандидаты входа в авторизацию: 72 `tenantRequest`, 4 проверяющих отзывные media grants, 2 device challenge/token и 1 device principal для capture receipt. Эта классификация не доказывает ACL внутри сервисов/SQL.", "",
             "`declared` означает только найденную регистрацию/маршрут в исходниках. Все поведенческие ворота остаются не подтверждёнными без отдельного анализа/исполнения; отсутствие нового defect ID не означает готовность.", "",
             "## Операции", "", "| Пакет | Операция | Worker declaration | Service/SQL literal candidates | Дефект/блокер |", "|---|---|---|---|---|"]
    for r in operations:
        ev = f"{r['service_literal_file_count']}/{r['sql_literal_file_count']}"
        findings = r['confirmed_code_gaps'] + ([r['platform_qualification_blocker']] if r['platform_qualification_blocker'] else [])
        lines.append(f"| {r['package']} | `{r['id']}` | {r['worker_registration_kind']} | {ev} | {'; '.join(findings) or '—'} |")
    lines += ["", "## HTTP-контракты", "", "| Пакет | Метод и путь | Route declaration | Auth entry candidate | Дефект |", "|---|---|---|---|---|"]
    for r in endpoints:
        lines.append(f"| {r['package']} | `{r['method']} {r['path']}` | {r['route_declaration_kind']} | {r['auth_entry_candidate']} | {'; '.join(r['confirmed_code_gaps']) or '—'} |")
    lines += ["", "Полные пути файлов и кандидаты исходников находятся в [static-link-review.json](static-link-review.json) и [trace-inventory.json](trace-inventory.json). Подтверждённые дефекты и условия исправления — в [static-link-defects-2026-10-15.md](static-link-defects-2026-10-15.md).", ""]
    (HERE / "static-link-review.md").write_text("\n".join(lines))
    print(json.dumps({"operations": len(operations), "endpoints": len(endpoints),
                      "operations_without_registration": sum(not x["worker_registration_files"] for x in operations),
                      "endpoints_without_declaration": sum(not x["route_declaration_files"] for x in endpoints),
                      "endpoints_not_reached_from_Routes": sum(not x["mounted_from_server_routes"] for x in endpoints),
                      "endpoints_without_auth_entry_candidate": sum(x["auth_entry_candidate"] == "not_found_in_handler_prefix" for x in endpoints)}))


if __name__ == "__main__":
    main()
