# STATUS — журнал проверок и состояние реализации

Здесь только факты о прогонах: дата, результат, ограничения. Контракты (что делать) — в файлах этапов 12–18. Числа из разных прогонов не суммировать: наборы пересекаются.

Новая запись: дата, команда, число PASS по каждому набору отдельно, что НЕ покрыто.

---

## Сводка по этапам (обновлять при закрытии/изменении статуса)

| Этап | Миграции | Состояние | Детали |
|---|---|---|---|
| 1А каталог | 02598, 02599 | закрыто, продажи закрыты | [11](11-implementation-progress.md) |
| 1Б.1 quota kernel | 02600, 02601 | закрыто, shadow | [12](12-quota-kernel.md) |
| 1Б.2 standard Apply | 02602, 02603 | закрыто, opt-in | [13](13-standard-apply-workflow.md) |
| 1Б.3 routed Apply | 02604 | закрыто, opt-in | [14](14-routed-apply-workflow.md) |
| 1Б.4 V2 render | 02605 | закрыто, default-disabled | [15](15-v2-render-workflow.md) |
| 1Б.5 Editor effects | 02606 | закрыто, default-disabled | [16](16-editor-effects-workflow.md) |
| 1Б.6 отдельные операции | 02607 (+ paid 02608) | закрыто, default-disabled | [17](17-standalone-operations.md) |
| 1Б.7 selected/final | 02609 | **приёмка не завершена**, OPEN-08 | [18](18-selected-final-operations.md) |
| 1В finance | — | не начато | [backlog/README.md](backlog/README.md) |
| 1В provider | — | не начато | [backlog/README.md](backlog/README.md) |
| 1В usage | — | не начато | [backlog/README.md](backlog/README.md) |
| 1В legal | — | не начато | [backlog/README.md](backlog/README.md) |

Шаблон новой записи приёмки — в разделе «Приёмка модуля» файла `TESTING.md`.

---

## Журнал прогонов

### 1Б.1 quota kernel — 21.10.2026

Источник: `12-quota-kernel.md`

Общий billing-прогон: **77 passed in 59.48s**, включая 38 новых contract/SQL сценариев и регрессию предыдущего этапа. Проверка полного head 02601 включает реальный цикл funding и settlement.

- `go -C backend test ./db` — PASS
- Оба generator `--check` — PASS
- Рабочая БД не мигрировалась

Этап 1Б.1 закрытого quota kernel завершён; включение workflow enforcement остаётся следующим отдельным этапом.

---

### 1Б.2 standard Apply — 21.10.2026

Источник: `13-standard-apply-workflow.md`

- Общий regression прогон catalog/kernel/workflow и прежних Apply adoption/failure tests: **106 passed in 161.09s**
- Финальный прогон новой границы (fresh-session replay, canonical policy digest, bounded restart recovery): **20 passed in 132.62s** — включает 18 SQL/API/concurrency cases и 2 contract checks. Эти два прогона пересекаются; counts не суммировать.
- `go test ./internal/billing ./internal/run ./internal/workflow ./internal/taskrecovery ./db` — PASS
- Три generator `--check` — PASS; embedded migration bundle содержит 02602/02603

Сбой migration-template harness: произошёл до выполнения SQL scenarios; итоговый прогон выполнен с обычными отдельными БД. Рабочая БД не изменялась.

Граница lifecycle/erasure остаётся отдельной задачей: kernel намеренно отказывает inactive/deleting tenant.

---

### 1Б.3 routed Apply — 21.10.2026

Источник: `14-routed-apply-workflow.md`

- Финальный набор нового этапа: **29 PASS за 128.14s**, включая реальные Go API и PostgreSQL races
- Прежние regression cases: **51 PASS** (непересекающийся поднабор общего regression)
- Go billing/run/v2sessions/workflow/taskrecovery/db: PASS
- Все четыре generator `--check`: PASS
- Миграция применялась только в одноразовых тестовых БД

---

### 1Б.4 V2 render — 21.10.2026

Источник: `15-v2-render-workflow.md`

- Новый этап: **30 PASS за 70.89s** (после уточнения partial-failure success contract)
- Прежние billing/Apply: **68 PASS** в общем прогоне **98 PASS за 168.25s** — новый набор пересекается; не суммировать 98+30
- Прежние V2 manifest/state/cancellation: **18 PASS за 129.85s** (непересекающийся набор)
- Итого различных scenarios: 30 + 68 + 18 = 116
- Обычный Go-прогон: billing/v2render/workflow/run/db PASS
- Все пять generator `--check` и diff whitespace checks — PASS

Pixel/GPU/Stripe network e2e в эти результаты не входят.

---

### TST-01 — e2e-фикстура — 01.11.2026

Источник: `backlog/BACKLOG-testinfra-1.md`, задача TST-01

- `tests/integration/test_billing_e2e_fixture.py`: **13 passed** — создание tenant, RLS-изоляция, FakeProvider, insert_period с офсетами, T8 (no secrets in repr)
- Статические проверки: 15/15 PASS (`check-static.sh`)
- Рабочая БД не мигрировалась

TST-02 заблокирован до STR-01 (fake provider Go-пакет).

---

### TST-03 — Makefile-цели test-billing-integration/e2e — 01.11.2026

Источник: `backlog/BACKLOG-testinfra-1.md`, задача TST-03

- `tests/contracts/test_billing_check_e2e_coverage.py`: **9 passed** (unit-тесты скрипта без БД и сети)
- Статические проверки: 15/15 PASS
- `test-billing-integration`: запускает `tests/integration/test_billing_*.py`, падает при `skipped>0`
- `test-billing-e2e`: запускает `tests/integration/test_billing_e2e_*.py` + `check-e2e-coverage.py`, падает при `skipped>0`
- `check-e2e-coverage.py --json` на текущем репо: 90 E-IDs в бэклоге, 0 покрыто (сценарии ещё не реализованы — ожидается до завершения групп finance/provider/usage/legal/api/stripe/ui)
- Real пробег STR-02 (provider contract): не запускался (TST-02 ждёт STR-01)

---

### TST-04 — billing-test-utils + locale keys — 01.11.2026

Источник: `backlog/BACKLOG-testinfra-1.md`, задача TST-04

- `apps/photographer/src/billing-locale-keys.dom.test.tsx`: **7 passed** (ключи billing_* в en/ru/bg консистентны)
- `cd apps/photographer && npm test -- --run`: **530 passed | 5 skipped** (hardware/GPU only, не billing)
- 7 billing UI locale keys добавлены в locales/en/ru/bg: nav_billing, billing_period_trial/active, billing_quota_credits_title/used_of/expires_never/resource_standard_photo

---

### TST-05 — record-evidence.py — 01.11.2026

Источник: `backlog/BACKLOG-testinfra-1.md`, задача TST-05

- `tests/contracts/test_billing_record_evidence.py`: **16 passed** (parser, verdict, append, no-overwrite)
- Статические проверки: 15/15 PASS

---

### UI-00 — Навигация и routes — 01.11.2026

Источник: `backlog/BACKLOG-ui-1.md`, задача UI-00

- `apps/photographer/src/billing-settings-nav.dom.test.tsx`: **5 passed** (роли owner/admin/editor/viewer видят ровно назначенные пункты)
- `apps/photographer/src/navigation-entry-links.dom.test.tsx`: **4 passed** (без регрессий, viewer видит базовые пункты)
- `apps/photographer/src/routes.test.ts`: **34 passed** (без регрессий)
- TypeScript: PASS
- Статические проверки: 18/18 PASS
- Изменения: workspace-access.ts (ownerRoutes/adminRoutes), navigation-targets.ts (роль-фильтр), navigation-entry-links.tsx, локали en/ru/bg (+nav_usage/nav_billing_payment/nav_privacy)

---

### UI-13 — Числа, доступность, уведомления 80/95/100% — 01.11.2026

Источник: `backlog/BACKLOG-ui-3.md`, задача UI-13

- `apps/photographer/src/billing-format.test.ts`: **15 passed** (parseBillingInt, formatGB/GiB/TB, formatMoney, quotaProgress)
- `apps/photographer/src/billing-quota-bar.dom.test.tsx`: **5 passed** (ProgressBar 0..1, overflow text, accessible role)
- `apps/photographer/src/billing-quota-notify.dom.test.tsx`: **6 passed** (dedup по resource/period/threshold, стабильность при rerender)
- TypeScript: PASS
- Статические проверки: 18/18 PASS
- Изменения: billing-format.ts, billing-quota-bar.tsx, billing-quota-notify.ts (новые файлы)

---

### UI-14 — Привязка к существующему Paper shell — 01.11.2026

Источник: `backlog/BACKLOG-ui-3.md`, задача UI-14

- `apps/photographer/src/billing-shell-bind.dom.test.tsx`: **5 passed** (criterии A/B/C/D)
- TypeScript: PASS
- Статические проверки: 18/18 PASS
- Изменения: workspace-access.ts (legal-onboarding в globalRoutes), legal-onboarding.tsx (без обязательного tenant), billing-change-confirm.tsx (useCASMutation), profile-delete.tsx (scoped статус удаления через request_id), billing-payment-details.tsx (NATIVE_PAYMENT_CAPABILITY=false), локали +3 ключа profile_delete_pending/failed/timeout
