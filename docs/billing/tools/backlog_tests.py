"""Задачи приёмки модулей (ACC-*) и тестовой инфраструктуры (TST-*) для gen-backlog.py.
Имена тестовых файлов существующих этапов проверяются по репозиторию: нет файла — генерация падает."""
import os, re, sys

PY = "scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider"
GO = "scripts/run-platform.sh go -C backend test"
STAGES = [
 ("1A", "Каталог (02598–02599)", ["tests/contracts/test_billing_catalog_contract.py"], ["tests/integration/test_billing_catalog.py"], [], "generate-billing-catalog.py", ""),
 ("1B1", "Quota kernel (02600–02601)", ["tests/contracts/test_billing_quota_contract.py"], ["tests/integration/test_billing_quota.py"], [], "generate-billing-quota.py", ""),
 ("1B2", "Standard Apply (02602–02603)", ["tests/contracts/test_billing_workflow_contract.py"], ["tests/integration/test_billing_apply.py"], ["./internal/billing", "./internal/workflow"], "generate-billing-workflow.py", ""),
 ("1B3", "SessionAnalysis.Apply и SaveEdits (02604)", ["tests/contracts/test_billing_routed_workflow_contract.py"], ["tests/integration/test_billing_routed_apply.py"], ["./internal/billing", "./internal/v2sessions"], "generate-billing-routed-workflow.py", ""),
 ("1B4", "V2 render (02605)", ["tests/contracts/test_billing_render_contract.py"], ["tests/integration/test_billing_render.py", "tests/integration/test_billing_render_go.py"], ["./internal/billing", "./internal/v2render"], "generate-billing-render.py", ""),
 ("1B5", "Editor effects (02606)", ["tests/contracts/test_billing_editor_contract.py"], ["tests/integration/test_billing_editor.py"], ["./internal/billing", "./internal/v2input"], "generate-billing-editor.py", "Фикстура: `tests/integration/billing_editor_fixture.py`. Native pixel execution и fresh public compiler не квалифицированы (`OPEN-10`), в приёмку не входят."),
 ("1B6", "Отдельные фото-операции и paid funding (02607–02608)", ["tests/contracts/test_billing_operations_contract.py", "tests/contracts/test_billing_paid_operation_contract.py"], ["tests/integration/test_billing_operations.py"], ["./internal/billing"], "generate-billing-operations.py", "Go-harness: `backend/internal/billing/operations_integration_test.go`. Скрипт проверки paid funding — по `contracts/README.md` (в `gen-briefs` указан как «не указан»)."),
 ("1B7", "Selected/final операции (02609)", ["tests/contracts/test_billing_selected_contract.py"], ["tests/integration/test_billing_selected.py", "tests/integration/test_billing_selected_upgrade_guards.py"], ["./internal/billing", "./internal/v2input", "./internal/workflow", "./internal/run", "./internal/v2render", "./db"], "generate-billing-selected.py", "**Выполняет владелец (у него PostgreSQL и Go): это и есть `T-01`/`OPEN-08`.** Обязательны кейсы `v28_open_eyes_selected_authority` и `v28_glare_completed_source_ready`."),
]
GROUPS = [
 ("finance", "Финансы (BILL-04)", "tests/contracts/test_billing_finance_contract.py", "tests/integration/test_billing_finance.py"),
 ("provider", "Provider outbox/inbox (BILL-07/08)", "tests/contracts/test_billing_provider_contract.py", "tests/integration/test_billing_provider.py"),
 ("usage", "Usage, storage, egress (BILL-05/06/09)", "tests/contracts/test_billing_usage_contract.py", "tests/integration/test_billing_usage.py"),
 ("legal", "Legal и privacy (BILL-11/12)", "tests/contracts/test_billing_legal_contract.py", "tests/integration/test_billing_legal.py"),
]
THEME_OF_GROUP = {"finance": ["general"], "provider": ["provider"], "usage": ["quota"], "legal": ["legal"], "stripe": ["provider"], "ui": ["ui"]}
THEMES = [("provider", r"Stripe|webhook|Checkout|SCA|Test Clock|Portal|dispute|refund|invoice|provider|payment|paid|Async pay"), ("legal", r"erasure|delet|consent|legal|cookie|onboarding|privacy|PII"), ("ui", r"\bUI\b|Paper|screen|экран"), ("quota", r"quota|lease|ticket|overrun|allowance|bucket|slot|photo|egress|storage"), ("catalog", r"seed|catalog|migration|backfill|Trial|plan")]

def scenario_ids(DOCS):
    out = {}
    pat = re.compile(r"^\|\s*(E\d{2}) ([^|]+)\|([^|]*)\|([^|]*)\|\s*$")
    for n in ("08-delivery-migrations-e2e.md", "10-audit-and-traceability.md"):
        for ln in open(os.path.join(DOCS, n), encoding="utf-8").read().split("\n"):
            m = pat.match(ln.strip())
            if m:
                txt = " ".join(m.group(i) for i in (2, 3, 4))
                th = next((t for t, rx in THEMES if re.search(rx, txt, re.I)), "general")
                out.setdefault(th, []).append(m.group(1))
    return out

def evidence(acc):
    return ("- Доказательство: блок `### %s — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). "
            "Числа разных наборов не складывать (`TESTING.md` T6)." % acc)

def build(DOCS, REPO, est, chunk):
    files = {}
    def pack(group, title, items):
        chunks, cur = [], []
        for it in items:
            if cur and est("\n".join(cur + it)) > chunk: chunks.append(cur); cur = []
            cur = cur + it
        if cur: chunks.append(cur)
        names = []
        for k, ch in enumerate(chunks, 1):
            nm = "BACKLOG-%s-%d.md" % (group, k); names.append(nm); files[nm] = "\n".join(["# Бэклог: %s, часть %d" % (title, k), ""] + ch)
        return names
    sc = scenario_ids(DOCS)
    A = []
    for sid, title, contract, integ, go, gen, note in STAGES:
        for f in contract + integ:
            if not os.path.exists(os.path.join(REPO, f)): sys.exit("backlog_tests: нет файла %s" % f)
        acc = "ACC-%s" % sid
        cmds = ["`python3 scripts/%s --check`" % gen, "`%s %s`" % (PY, " ".join(contract)), "`%s %s` (нужна БД: `make dev-infra`)" % (PY, " ".join(integ))]
        if go: cmds.append("`%s %s` (нужны PostgreSQL и Go)" % (GO, " ".join(go)))
        A.append(["### %s — приёмка этапа %s" % (acc, title), "",
                  "- Выполняет: человек или CI с PostgreSQL и Go. Модель только готовит блок доказательства.",
                  "- Условие входа: все задачи этапа закрыты, их тесты лежат в репозитории (R33).",
                  "- Команды (по порядку, остановиться на первой ошибке): " + "; ".join("%d) %s" % (i, c) for i, c in enumerate(cmds, 1)) + ".",
                  "- Ожидается: `failed=0` и `skipped=0` в каждом наборе (`TESTING.md` T5); skip = провал.",
                  evidence(acc)] + (["- Примечание: %s" % note] if note else []) + [""])
    for g, title, ct, it in GROUPS:
        acc = "ACC-%s" % g
        ids = sorted(set(sum((sc.get(t, []) for t in THEME_OF_GROUP[g]), [])))
        A.append(["### %s — приёмка группы «%s»" % (acc, title), "",
                  "- Условие входа: закрыты все задачи `%s-*` из `backlog/` вместе со своими тестами (R33) и `%s-00` (каркас)." % (g[:3].upper(), g[:3].upper()),
                  "- Команды: 1) `python3 scripts/generate-billing-%s.py --check`; 2) `%s %s`; 3) `%s %s` (нужна БД)." % (g, PY, ct, PY, it),
                  "- Ожидается: `failed=0`, `skipped=0`; для каждой таблицы и функции группы есть тест по `TESTING.md` (T4).",
                  "- Сценарии e2e этой темы (подсказка по ключевым словам, уточнить по 08/10): %s." % (", ".join(ids) or "нет"),
                  evidence(acc), ""])
    A.append(["### ACC-api — приёмка API (BILL-10)", "",
              "- Условие входа: закрыты все `API-*` с Go-тестами handler (R33).",
              "- Команды: 1) `%s ./internal/httpapi ./internal/billing`; 2) `make contracts-check`; 3) contract-тесты реестров endpoints и schemas." % GO,
              "- Ожидается: `ok` по всем пакетам, drift DTO нет, endpoint без handler не зарегистрирован.", evidence("ACC-api"), ""])
    A.append(["### ACC-stripe — приёмка Stripe (fake, затем sandbox)", "",
              "- Условие входа: закрыты `STR-00..STR-13`, `STR-16`, `TST-02`; ключей нет и не нужно.",
              "- Команды: 1) Go-тесты пакета провайдера на fake (путь — из `STR-01`); 2) общий контрактный набор `TST-02` на fake.",
              "- Ожидается: все PASS без сети и без секретов. Прогон того же набора на real — отдельная запись после `STR-14` (ключи, `OPEN-14`).",
              "- Сценарии e2e: %s (сначала fake)." % (", ".join(sorted(set(sc.get("provider", [])))) or "нет"), evidence("ACC-stripe"), ""])
    A.append(["### ACC-ui — приёмка Paper UI", "",
              "- Условие входа: закрыты все `UI-*` с dom-тестами (R33), `TST-04`.",
              "- Команды: 1) `cd apps/photographer && npm test -- --run`; 2) `npm run typecheck`; 3) `make test-web`.",
              "- Ожидается: все PASS; ключи bg/en/ru совпадают; native payment capability=false (`OPEN-05`).",
              "- Сценарии e2e (UI-часть): %s." % (", ".join(sorted(set(sc.get("ui", [])))) or "нет"), evidence("ACC-ui"), ""])
    A.append(["### ACC-e2e — финальный прогон E01–E90", "",
              "- Условие входа: приняты все остальные `ACC-*`, закрыты `TST-01`, `TST-03`.",
              "- Команды: `make test-billing-e2e` (создаётся в `TST-03`) — все `tests/integration/test_billing_e2e_*.py` на одноразовой БД.",
              "- Ожидается: для каждого из %d сценариев есть положительный и отрицательный тест с docstring по шаблону; `failed=0`, `skipped=0`; список сценариев без теста пуст (проверяет `TST-03`)." % sum(len(v) for v in sc.values()),
              "- Не входит: real-Stripe (`STR-14`), native pixel (`OPEN-10`), live-продажи (`OPEN-09`).", evidence("ACC-e2e"), ""])
    T = [
     ["### TST-01 — общая e2e-фикстура", "",
      "- Цель: одна фикстура для всех `test_billing_e2e_*.py`: одноразовая БД, tenant, роли, активный профиль, fake-провайдер, управляемые часы.",
      "- Вход: `tests/integration/billing_editor_fixture.py` (образец формы), `tests/integration/conftest.py` (фикстура `database`), `TESTING.md`.",
      "- Выход: `tests/integration/billing_e2e_fixture.py` + тест самой фикстуры `tests/integration/test_billing_e2e_fixture.py`.",
      "- Критерий: A. Фикстура создаёт и удаляет одноразовую БД, рабочую не трогает (R30). B. Два tenant для проверки RLS. C. Без БД — skip, как у `database`. D. Секретов нет (T8).",
      "- Тесты (R33): `test_billing_e2e_fixture.py` — создание tenant, изоляция, откат.", ""],
     ["### TST-02 — контрактный набор провайдера (fake и real)", "",
      "- Цель: один набор проверок поведения провайдера (идемпотентность, timeout после успеха, неизвестный объект), запускаемый на fake и позже на real.",
      "- Вход: результат `STR-01`, `05-stripe-test-setup.md`, `TESTING.md` T7.",
      "- Выход: `contract_test.go` в пакете провайдера (путь — из `STR-01`): функция `RunProviderContract(t, factory)`; вызов для fake без условий, для real — только при наличии ключей.",
      "- Критерий: A. Fake PASS без сети. B. Real пропускается явно, и это записано как «не запускалось» в `STATUS.md`. C. Один и тот же набор — T7.",
      "- Тесты (R33): сам набор; проверка, что fake нарушает контракт при сломанной идемпотентности (негативный тест набора).", ""],
     ["### TST-03 — команды `make test-billing-integration` и `make test-billing-e2e`", "",
      "- Цель: единые команды приёмки, падающие при skip и при сценарии E01–E90 без теста.",
      "- Вход: `Makefile` (цель `test-billing-contracts` как образец), `TESTING.md`.",
      "- Выход: цели в `Makefile` и скрипт `docs/billing/tools/check-e2e-coverage.py` (каждый E-ID из 08/10 есть как `test_eNN_*` и имеет docstring с «Положительный» и «Отрицательный»).",
      "- Критерий: A. Команда падает при `skipped>0`. B. `check-e2e-coverage.py` падает при пропущенном E-ID. C. Рабочая БД не используется.",
      "- Тесты (R33): negative-проверка скрипта на временном каталоге с пропущенным сценарием.", ""],
     ["### TST-04 — helper для dom-тестов Paper UI", "",
      "- Цель: общий helper рендера экрана с провайдерами Paper, локалью, tenant и подменой API; проверка ключей локалей.",
      "- Вход: `apps/photographer/src/quotas-locale.dom.test.tsx` (образец), `paper-provider`, `locale-context`, `owner-api`.",
      "- Выход: `apps/photographer/src/billing-test-utils.tsx` и тест `billing-locale-keys.dom.test.tsx` (одинаковый набор ключей биллинга в bg/en/ru).",
      "- Критерий: A. `cd apps/photographer && npm test -- --run` PASS. B. Лишний или отсутствующий ключ в одной локали роняет тест.",
      "- Тесты (R33): `billing-locale-keys.dom.test.tsx`.", ""],
     ["### TST-05 — запись доказательства приёмки", "",
      "- Цель: не переписывать числа руками: скрипт принимает вывод pytest/go test и дописывает блок в `STATUS.md`.",
      "- Вход: формат блока из `TESTING.md`, `STATUS.md`.",
      "- Выход: `docs/billing/tools/record-evidence.py <ACC-id> <файл вывода>`; unit-тест `tests/contracts/test_billing_record_evidence.py`.",
      "- Критерий: A. Из строки `12 passed, 2 skipped` получается `skipped=2` и пометка «приёмка не пройдена». B. Числа разных запусков не суммируются (T6). C. Повторная запись не затирает старые блоки.",
      "- Тесты (R33): unit-тест на три формата вывода (чистый, со skip, с failed).", ""],
    ]
    acc_names = pack("acceptance", "Приёмка модулей (ACC)", A)
    tst_names = pack("testinfra", "Тестовая инфраструктура (TST)", T)
    idx = [("acceptance", "ACC", "—", "%d задач" % len(A), acc_names), ("testinfra", "TST", "—", "%d задач" % len(T), tst_names)]
    return files, idx, dict(acc=len(A), tst=len(T))
