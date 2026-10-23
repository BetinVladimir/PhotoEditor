# STATUS — журнал проверок и состояние реализации

Здесь только факты о прогонах: дата, результат, ограничения. Контракты (что делать) — в файлах этапов 12–18. Числа из разных прогонов не суммировать: наборы пересекаются.

Новая запись: дата, команда, число PASS по каждому набору отдельно, что НЕ покрыто.

## Сводка по этапам

| Этап | Миграции | Состояние |
|---|---|---|
| 1А каталог | 02598, 02599 | закрыто, продажи закрыты |
| 1Б.1 quota kernel | 02600, 02601 | закрыто, shadow |
| 1Б.2 standard Apply | 02602, 02603 | закрыто, opt-in |
| 1Б.3 routed Apply | 02604 | закрыто, opt-in |
| 1Б.4 V2 render | 02605 | закрыто, default-disabled |
| 1Б.5 Editor effects | 02606 | закрыто, default-disabled |
| 1Б.6 отдельные операции | 02607 (+ paid 02608) | закрыто, default-disabled |
| 1Б.7 selected/final | 02609 | **приёмка не завершена**, см. OPEN-08 |

Полный журнал по этапам: [11-implementation-progress.md](11-implementation-progress.md).


---

<!-- перенесено из 12-quota-kernel.md -->

## 12 — Результат проверки 21.10.2026

Общий billing-прогон: **77 passed in 59.48s**, включая 38 новых contract/SQL сценариев и регрессию предыдущего этапа. Проверка полного head02601 включает реальный цикл funding и settlement. `go -C backend test ./db` — PASS; оба generator `--check` — PASS. Рабочая БД не мигрировалась. Этап 1Б.1 закрытого quota kernel завершён; включение workflow enforcement остаётся следующим отдельным этапом.


---

<!-- перенесено из 13-standard-apply-workflow.md -->

## 13 — Проверено 21.10.2026

- Общий regression прогон catalog/kernel/workflow и прежних Apply adoption/failure tests: **106 passed in 161.09s**.
- После уточнений fresh-session replay, canonical policy digest и bounded restart recovery финальный прогон новой границы: **20 passed in 132.62s**, без migration-template оптимизации; включает 18 SQL/API/concurrency cases и 2 contract checks. Эти прогоны пересекаются; их counts нельзя складывать как уникальные тесты.
- `go test ./internal/billing ./internal/run ./internal/workflow ./internal/taskrecovery ./db` — PASS; настоящий Go API integration отдельно реально исполняется внутри SQL harness с PHOTO_TEST_DATABASE_URL и fixture, а не считается проверенным по skip без окружения.
- Три generator `--check` — PASS; embedded migration bundle содержит 02602/02603; `git diff --check` для изменённых файлов — PASS.

Сбой ускоренного migration-template harness на проверке завершения baseline connection произошёл до выполнения SQL scenarios; итоговый прогон выполнен с обычными отдельными БД. Инфраструктуру templates и рабочую БД это изменение не модифицирует.

Граница lifecycle/erasure остаётся отдельной задачей: kernel намеренно отказывает inactive/deleting tenant. До включения коммерческого cohort erasure coordinator должен получить reviewed funding-drain/terminal-cleanup protocol; завершённость удаления профиля/GDPR из этих workflow tests не следует.


---

<!-- перенесено из 14-routed-apply-workflow.md -->

## 14 — Проверено 21.10.2026

- Финальный набор нового этапа: **29 PASS за 128.14s**, включая реальные Go API и PostgreSQL races.
- Прежние workflow/contract/adoption/failure/V22 regression cases: **51 PASS**. Это непересекающийся с новым набором поднабор общего regression прогона; результаты нового набора взяты из финального повторного прогона после исправления admission fixture.
- Go billing/run/v2sessions/workflow/taskrecovery/db: PASS. Все четыре generator --check: PASS.
- Миграция применялась только в одноразовых тестовых БД. Реальное исполнение пиксельных worker и Stripe network calls в эти результаты не входят.


---

<!-- перенесено из 15-v2-render-workflow.md -->

## 15 — Проверено 21.10.2026

- Новый этап: **30 PASS за 70.89s** в финальном повторном прогоне после уточнения partial-failure success contract.
- Прежние billing contracts и Apply/SessionAnalysis/SaveEdits: **68 PASS** в общем прогоне **98 PASS за 168.25s**. Новый набор пересекается с этим общим прогоном; не суммировать 98+30.
- Прежние V2 manifest/state/cancellation/submission/finalizer tests: **18 PASS за 129.85s** (отдельный непересекающийся набор).
- Всего проверено 116 различных contract/integration scenarios:30 нового этапа +68 billing regression +18 V2 regression.
- Обычный Go-прогон без временных modfile: billing/v2render/workflow/run/db PASS. Все пять generator --check и diff whitespace checks PASS.

Промежуточные попытки обнаруживали ошибки подготовки multi-frame frozen snapshot, конфликт SQL alias и ошибки template isolation при изменении source/параллельных pytest. Fixtures/alias исправлены; финальные прогоны выполнены последовательно на зафиксированной миграции. Никакие проверки не ослаблялись и рабочая БД не обновлялась. Ранее мешавшие общему Go build сторонние dependency/type ошибки в текущем дереве устранены; финальный обычный Go-прогон проходит. Pixel/GPU/Stripe network e2e в эти результаты не входят.

Уточнение следующего этапа: миграция 02606 добавляет dedicated Editor ticket и защищает typed final child отдельным Editor opt-in. Актуальная граница описана в [16-editor-effects-workflow.md](16-editor-effects-workflow.md); самостоятельный Render по-прежнему не списывает за Editor child.
