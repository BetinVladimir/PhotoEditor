# MVP-03. Приложение фотографа (`apps/photographer`)

Целевая платформа MVP — **web** (Expo Router + react-native-web). Нативные сборки — после MVP.

Состояние: 42 экрана / 126 языковых URL, 585 production-файлов, `tsc --noEmit` проходит. Крупнейшие файлы по размеру: `src/owner-api.ts` (≈124 КБ), `screens/editor/[version].tsx` (≈98 КБ), `screens/collections/[id]/index.tsx` (≈30 КБ), `screens/reviews/[id].tsx` (≈25 КБ), `src/routes.ts` (≈25 КБ).

| ID | Приоритет | Работа | Трудоёмкость |
|---|---|---|---|
| P-1 | **P0** | Allowlist маршрутов и навигации MVP | S–M |
| P-2 | **P0** | Измерение контраста/размера шрифта и e2e главного пути | M (см. [06](06-testing-ci-e2e.md)) |
| P-3 | P1 | Форматтер + линтер, единичный «reformat» коммит | S |
| P-4 | P1 | Единый журнал мутаций вместо ≈55 attempt-store | M–L |
| P-5 | P1 | Разделение `OwnerAPI` на доменные клиенты | M |
| P-6 | P1 | Светлая/тёмная тема, токены вместо цветов в стилях | M |
| P-7 | P1 | Надёжность и скорость импорта (адаптивная параллельность, хеширование, IndexedDB) | M |
| P-8 | P1 | Воспроизводимая web-сборка и деплой | S |
| P-9 | P2 | Декомпозиция больших экранов (`editor`, `collections`, `reviews`) | L |
| P-10 | P2 | Единая обработка ошибок и клиентская телеметрия | M |

## P-1. Allowlist маршрутов MVP (P0)

* **Проблема:** в `apps/photographer/app/[locale]/` присутствуют маршруты функций, отложенных решением MVP (V23 обучение стиля, V30 interchange, V31 подпись): `styles/[id]/training.tsx`, `interchange/*` (8 файлов: `new`, `format`, `import-format`, `prepare`, `upload`, `[id]`, `prepared/[id]`, `masks/[id]`), `exports/sign.tsx`. Защита опирается на серверные capability; кнопки/ссылки на них могут показываться.
* **Изменить:**
  1. Новый `apps/photographer/src/mvp-profile.ts`: `export const MVP_DEFERRED_PREFIXES = ['interchange', 'exports/sign', 'styles/*/training']` + `isRouteEnabled(path)`; профиль выбирается `EXPO_PUBLIC_PROFILE=mvp|full` (по умолчанию `mvp` в production-сборке).
  2. `apps/photographer/src/routes.ts` — единый источник ссылок: вместо прямых строк в компонентах использовать функции `routes.interchange(...)`, возвращающие `null` вне профиля; компоненты, получив `null`, не рисуют ссылку (пример потребителей: `src/interchange-*-screen.tsx`, `src/collection-organization.tsx`, `src/delivery-package-screen.tsx` — найти `grep -rn "interchange" src screens`).
  3. `apps/photographer/app/[locale]/_layout.tsx`: guard — если `!isRouteEnabled(segments)` → `router.replace('/')` и страница «Недоступно в этой версии» (ключ `feature_not_in_mvp`, EN/RU/BG в `locales/*/common.json`).
  4. Для сборки без отложенных экранов: вынести их в группу `app/[locale]/(labs)/…` и исключать из `expo export` по профилю (`app.config.js`: `extra.profile`, `metro.config.js`: блок `resolver.blockList` для `(labs)` при `profile=mvp`).
* **Согласование с сервером:** профиль `PHOTO_PROFILE=mvp` ([02 B-4](02-backend-and-contracts.md)). Контрольный тест: `src/mvp-profile.test.ts` — в `mvp` ни один маршрут из `routes.ts` не ведёт на отложенный префикс.
* **Не удалять код** (требование проекта) — только не включать в сборку/навигацию.

## P-2. UI-проверки (P0)

Выполнить пункты из `docs/ui-audit/PLAN.md`: UI-01 (политика-тест на `screens/`), UI-02 (светлая тема), UI-14 (измерения). Автоматизация — в [06-testing-ci-e2e.md](06-testing-ci-e2e.md): `@axe-core/playwright` для контраста и проверка `minimum font-size` через `getComputedStyle` на каждом выпускаемом экране.

## P-3. Форматтер и линтер (P1)

* **Файлы:** `apps/photographer/package.json` (скрипты `lint`, `format`, `format:check`), `.prettierrc.json` (`printWidth: 110`, `singleQuote: true`, `semi: true`), `eslint.config.mjs` (`typescript-eslint`, `eslint-plugin-react-hooks`, `no-restricted-imports` для примитивов `react-native` — заменяет `src/paper-ui-policy.test.ts`; правила `max-len` 140 (warn), `max-lines-per-function` 150 (warn), `complexity` 20 (warn)).
* **Порядок:** 1) заморозить функциональные PR; 2) `prettier --write app screens src`; 3) отдельный коммит + `.git-blame-ignore-revs`; 4) подключить `lint`/`format:check` в CI. Аналогично `apps/portal` и `packages/api-client`.
* **Риск:** тесты, сопоставляющие исходный текст (поиск по `*.test.ts` на `readFileSync(…/src/…)`: `grep -rln "readFileSync" apps/photographer/src --include=*.test.*`) могут ломаться от форматирования — обновить их в том же коммите.

## P-4. Единый журнал мутаций (P1)

* **Проблема:** `src/auth-context.tsx:74` — один `Promise.all` из ≈55 вызовов `clear*Attempts(AsyncStorage, origin)`; 112 файлов в `src/` содержат `attempt` в имени. Каждый `*-attempt-store.ts` повторяет: заморозка запроса, ключ идемпотентности, статус, чтение квитанции, очистка.
* **Изменить:**
  1. Новый `src/mutation-journal.ts`: `createJournal<Req, Receipt>({ kind, scope, freeze(req), readReceipt(raw, frozen) })` → `begin(request): Attempt`, `resume(): Attempt | null`, `complete(attempt, receipt)`, `abandon(attempt)`, `list()`. Ключи хранилища: `pe:j:<origin>:<kind>:<id>`; один общий `clearAll(origin)`.
  2. Заменить вызов в `auth-context.tsx:74` на `await journals.clearAll(origin)` + регистрация журналов при создании (реестр `journalRegistry`), чтобы новый журнал не мог «забыться» при выходе из учётной записи.
  3. Миграция по одному: пилот — `catalog-create-attempt-store.ts`, `collection-create-attempt-store.ts`, `export-batch-attempt-store.ts`; существующие `*-attempt-store.test.ts` сохранить как характеризационные.
  4. Адаптер хранилища: для web — IndexedDB (журналы загрузок, тысячи записей), для native — AsyncStorage; интерфейс `JournalStorage` (`get/set/remove/keys`).
* **Критерий:** число модулей `*-attempt-store.ts` уменьшается ≥ в 5 раз; выход из учётной записи гарантированно очищает всё (тест перебирает реестр).

## P-5. Разделение `OwnerAPI` (P1)

* **Где:** `apps/photographer/src/owner-api.ts` — `export class OwnerAPI` (строка 122, ≈200 методов).
* **Изменить:** вынести методы в `src/api/{projects,collections,uploads,exports,deliveries,reviews,editor,styles,settings,interchange}.ts` как функции `(client, tenant, …)` (часть уже так: `signExport`, `signingPolicies` — строка 134–135); `OwnerAPI` оставить фасадом с делегированием, чтобы не менять потребителей одним коммитом. Общий HTTP-слой (`OwnerClient`/`mutate`/`get`) — единственный файл с `fetch`.
* **Связь с `packages/api-client`:** портал и фотограф используют разные клиентские слои; общие парсеры (`read*Response`) переносить в `packages/api-client/src`.
* **Тесты:** `owner-api*.test.ts`, `*-response.test.ts` — без изменений поведения.

## P-6. Темы и токены (P1)

* **Где:** `src/paper-theme.ts` (`photographerLightTheme` определена, не используется), `src/paper-provider.tsx` (`PhotographerPaperProvider`, выбор темы), `app/_layout.tsx` (фон корня), экраны со «зашитыми» цветами — например `src/auth-context.tsx` (`backgroundColor: '#222'`), `screens/imports/new.tsx` (`'#201a2b'`), поиск: `grep -rn "#[0-9a-fA-F]\{3,8\}" app screens src --include=*.tsx | grep -v test`.
* **Изменить:** `useColorScheme()` + сохранённая настройка (`settings/profile.tsx`); заменить литералы на `useTheme().colors.*`; добавить `scripts/check-contrast.mjs` — расчёт WCAG-контраста для всех пар «текст/фон» токенов обеих тем, падает при < 4,5:1 (3:1 для крупного текста).
* **Тесты:** снимок токенов, unit на контраст, e2e axe.

## P-7. Импорт: надёжность и скорость (P1)

* **Где:** `screens/imports/new.tsx` (`add()`), `src/uploads.ts` (`webSource`, `nativeSource`), `src/upload-part-transport.ts` (`putUploadPart`: тайм-аут части 120 с, до 16 MiB), `src/upload-resume-policy.ts`, `src/upload-storage.ts`, `src/upload-source-integrity.ts`.
* **Факт:** параллельность фиксирована (строка локали `import_concurrency`: «два файла × две части»); перед загрузкой считается SHA-256 всего файла (`setHashing(file.name)`), журнал — AsyncStorage (на web — `localStorage`).
* **Изменить:**
  1. Адаптивная параллельность частей: стартовать с 2×2, увеличивать до 4×4 при RTT < 100 мс и стабильных частях, уменьшать при ошибках/тайм-аутах; настройки — в `src/upload-resume-policy.ts`.
  2. Хеширование: потоковое по `File.slice` блоками 8–16 MiB с `crypto.subtle`/WASM-SHA256 в Web Worker (не блокировать UI); одновременно с первыми частями загрузки (серверное сравнение SHA на `complete`).
  3. Журнал загрузок — IndexedDB (см. P-4, адаптер).
  4. Стресс-тест: 3 000 файлов × 25 МБ в e2e (или синтетически) — без `QuotaExceededError`, без замороженного UI > 100 мс, корректное возобновление после перезагрузки вкладки.
* **Критерий:** загрузка 500 RAW (50–80 МБ) на канале 200 Мбит/с насыщает ≥ 70 % канала.

## P-8. Воспроизводимая web-сборка (P1)

* **Проблема:** `deploy/framevela/photographer/wrangler.jsonc` ссылается на `../../../work/photographer-web-localized-r2` (неотслеживаемый каталог).
* **Изменить:** `apps/photographer/package.json`: `"build:web": "expo export --platform web --output-dir dist"` + проверка размера; `wrangler.jsonc` → `assets.directory: "../../../apps/photographer/dist"` либо контейнер `deploy/images/photographer` (статические файлы за Caddy) — выбрать **один** путь для MVP (см. [05](05-deploy-and-ops.md)); переменные окружения (`EXPO_PUBLIC_API_ORIGIN`) — из окружения сборки, не из репозитория.

## P-9. Декомпозиция больших экранов (P2)

* `screens/editor/[version].tsx` (≈98 КБ) → `screens/editor/` пакет: `EditorScreen.tsx` (композиция), `useEditorState.ts`, `panels/*` (уже есть `src/editor-panels.tsx` и `editor-*` — довести), `canvas/*`, `commands/*`.
* `screens/collections/[id]/index.tsx` (≈30 КБ), `screens/reviews/[id].tsx` (≈25 КБ), `screens/capture/[id].tsx` (≈24 КБ), `screens/exports/index.tsx` (≈18 КБ): логика → хуки, представление → компоненты ≤ 300 строк; цель по линтеру — `max-lines-per-function` ≤ 150.

## P-10. Ошибки и телеметрия (P2)

* **Где:** `app/[locale]/_layout.tsx` (ErrorBoundary уровня маршрута), `src/read-resource-state.tsx`, `src/use-read-resource.ts`.
* **Изменить:** единый `ErrorBoundary` + отправка обезличенных клиентских ошибок (код, маршрут, версия сборки, `request_id` из ответа API) на `POST /api/v1/client-errors` (серверный обработчик с rate-limit, [02 B-1](02-backend-and-contracts.md)); включить в e2e проверку «ни одной необработанной ошибки консоли».
