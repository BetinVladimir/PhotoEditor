# 02. Качество кода

Оценка по компонентам: **что хорошо**, **что плохо**, **конкретные места**. Метрики получены подсчётом по исходникам (не через линтеры, которых в репозитории нет).

## Общая картина

* Логика, как правило, продумана: явная валидация входов, ограничения размеров, проверки идентичности файлов (`st_dev/st_ino`), единообразные коды ошибок, отсутствие утечки путей/SQL в сообщениях об ошибках, детерминированные канонические JSON-представления.
* **Форма кода — главный риск сопровождения.** Нет ни форматтера, ни линтера в репозитории (нет `.eslintrc`, `.prettierrc`, `.golangci.yml`, `.editorconfig`; `ruff` указан только как dev-зависимость в `workers/pyproject.toml`, секции `[tool.ruff]` нет). Результат: строки по 300–1 000 символов, ряд файлов явно не отформатирован.
* Много «защитного» кода, повторяющегося как шаблон (проверка тех же полей на нескольких слоях). Это повышает безопасность, но кратно увеличивает объём и вероятность расхождения копий.

| Метрика | Значение | Источник |
|---|---|---|
| Строки >200 символов, production-код photographer (`app`, `screens`, `src`, без тестов) | **2 271** | `grep '.\{200,\}'` |
| То же, portal | 59 | там же |
| То же, `workers/photo_worker` (Python) | 226 | там же |
| Самые длинные Go-функции | `catalogRoutes` 3 811 строк, `ApplyCoordinator.build` 2 317, `runSchedulerWithDeliverySigning` 1 395, `runWithAPIDependencies` 638, `StyleAnalysisCoordinator.build` 616, `run.Service.apply` 554 | `backend/internal/httpapi/catalog.go`, `workflow/apply.go:834`, `workflow/service.go`, `cmd/photo/main.go`, `workflow/style_analysis.go`, `run/apply.go` |
| Python `build_worker` | ≈510 строк, ≈40 именованных параметров | `workers/photo_worker/runtime/service.py:343-852` |
| Python `TaskDatabase` | ≈100 однотипных методов вызова SQL-функций в одном файле 1 164 строки | `workers/photo_worker/runtime/database.py` |
| TS `OwnerAPI` | один класс, ≈200 методов, файл 1 079 строк, часть методов в одной строке | `apps/photographer/src/owner-api.ts:122` |

## Backend (Go)

**Хорошо**

* Чёткое разделение на пакеты по доменам (`tenant`, `collection`, `upload`, `gateway`, `queue`, `billing`, `portal`, …), sqlc для типизированного SQL, генерация контрактов и `--check`-режимы генераторов.
* Единый тип ошибок `domain.NewError(code, status)`; обязательные `If-Match`/`Idempotency-Key` (`httpapi/catalog.go: revision`); cookie `HttpOnly`, `SameSite=Lax`, `Secure` в production (`httpapi/auth.go:25`).
* OTP-лимиты в БД (по IP и email, окно 1 час) — `auth/service.go:140-158`.
* Рабочие тесты очень строгие: отдельные SQL-интеграционные, wire-тесты gateway, тесты отмены, retry, обрыва потока.

**Плохо**

* **God-функции.** `catalogRoutes` — ≈3,8 тыс. строк регистрации маршрутов с inline-обработчиками; `ApplyCoordinator.build` — ≈2,3 тыс. строк. Их невозможно ревьюить и безопасно менять; каждое изменение влечёт широкий регресс.
* `cmd/photo` содержит ≈60 файлов `*_installation.go`/`*_create.go` — по сути ручные «сборочные» цепочки на каждую операцию. Нет единой модели «операция = дескриптор + обработчики».
* `context.Background()` вне тестов — 88 мест: нужно проверить, что нет потери deadline/cancel в обработчиках (кандидаты: `workflow/service.go`, `maintenance`).
* `panic(` вне тестов — 7 мест: проверить, что они только в инициализации.
* В `backend/_work` (≈1,8 МБ) коммитятся «scratch records» по соглашению репозитория (коммит `06145ca9`): это артефакты процесса, а не кода.

## Worker (Python)

**Хорошо**

* Модульное разделение операций (`operations/*`), контрактов (`contracts/*`), моделей (`models/*`), runtime (`runtime/*`).
* Транспорт и исполнитель написаны аккуратно: `Transport.consume` корректно разделяет pump AMQP и вычисление в отдельном потоке, ack только после фиксации результата; `Executor.process` разбирает исключения по классам и никогда не сохраняет пути/SQL в ошибках; жёсткие проверки идентичности файла перед выгрузкой.
* Ленивые импорты тяжёлых операций (`_lazy_operation_factory`) — старт воркера не тянет torch без необходимости.

**Плохо**

* Стиль «сжатого» Python: `if…:raise …` в одну строку, нет пробелов вокруг операторов, 200+ символьные строки (`runtime/service.py`, `runtime/database.py`, `runtime/gateway.py`).
* `build_worker`/`run_worker` дублируют сигнатуру из ≈40 параметров (`service.py:343` и `service.py:853`); проверки совместимости «resource_class × backend» повторены ≈25 раз вручную. Нужна декларативная таблица совместимости и объект `Installations`.
* `TaskDatabase` как «толстый» адаптер: добавление операции = новый метод с 10–13 позиционными `%s` (легко перепутать порядок аргументов). Рекомендуется типизированные DTO/`NamedTuple` + единая функция вызова.
* Исключения `except Exception` в нескольких критичных местах превращают любую ошибку в `INTERNAL_ERROR` (допустимо), но heartbeat-ветка `except Exception: ctx._lost.set(); return` теряет различие «сеть недоступна» vs «lease действительно потеряна» (`executor.py:190-192`).

## Приложение фотографа (Expo/React Native)

**Хорошо**

* `strict: true` в `tsconfig.json`; `tsc --noEmit` — без ошибок (12 с).
* Запрет «сырых» RN-примитивов в пользу Paper соблюдён на уровне импортов (см. `docs/ui-audit/REPORT.md`).
* Серьёзное отношение к отмене запросов (`AbortSignal` повсюду), проверке формы ответов (`read*Response`), защите от смены сессии (Proxy в `auth-context.tsx: useAuth`).

**Плохо**

* **Плотность и размер.** 585 production-файлов, 112 файлов в `src/` содержат `attempt` в имени (вместе с тестами); `owner-api.ts` — один «божественный» класс; многие методы — одна строка >300 символов (пример: `owner-api.ts:976-981`).
* **Дублирование шаблона «attempt store».** Для каждой мутации свой модуль хранения повторов в AsyncStorage + свой `clear*Attempts` (≈55 вызовов в одном `Promise.all`, `auth-context.tsx:74`). Добавление фичи = ещё один модуль + правка списка очистки (легко забыть → утечка данных между учётками).
* Экраны (`screens/*`) — 390 КБ в 42 файлах (по данным UI-ревью): крупные компоненты с логикой, навигацией и стилями вместе (пример: `screens/imports/new.tsx` — 34 «строки» по 200–1 000 символов, включая `add()`).
* Тема: только тёмная (`photographerLightTheme` определена, не используется) — `docs/ui-audit/REPORT.md` F-02; цвета захардкожены в стилях (F-03).
* Политика-тест Paper не покрывает `screens/` (F-01 того же отчёта).

## Портал (Next.js)

**Хорошо**

* Малый объём (≈1,2 тыс. строк), чёткое разделение `lib/*` (чистые функции и их тесты) и компонента; секрет шаринга передаётся во фрагменте URL (`secretFromFragment`), SSR-оболочка содержит только идентификаторы (`s/[share_id]/page.tsx`); `Referrer-Policy: no-referrer`, `nosniff` (`next.config.ts`).
* Строгая проверка форм ответов (`lib/response.ts`, `download-link.ts`, `download-state.ts`).
* Виртуализация сетки (`virtual-grid.ts`), кэш медиа-ссылок (`media-cache.ts`).

**Плохо**

* `components/Portal.tsx` (350 «строк», 29 `useState/useRef`) — один компонент держит сессию, коллекцию, медиа-окно, выбор, скачивание, загрузку, lightbox и пять `AbortController`. Нужна декомпозиция на хуки/редьюсер (`usePortalSession`, `useMediaWindow`, `useDownloadJob`, `useGuestUpload`).
* Жёстко прописанный домен `https://framevela.com` в `LocaleLinks.tsx:10` и в `generateMetadata` (`s/[share_id]/page.tsx`, `[locale]/page.tsx`) — ломает staging/self-host и вводит поисковые метаданные (`canonical`, `alternates`) на страницах с `robots: noindex`.
* Именование продукта непоследовательно: «PhotoEditor» (репозиторий, пакеты `@photoeditor/*`), «Framevela» (домен, Cloudflare-воркеры, `deploy/framevela`). Нужно единое имя в пакетах, образах, i18n и метаданных.
* Тесты — только unit-функции `lib/*` и два компонента (`AccessibleLightbox`, `LocaleLinks`); сам `Portal.tsx` как целое не тестируется.

## Документация и процесс

* Документация обширна (≈113 элементов в `docs/implementation-v2` плюс биллинг, видео, UI-аудит) и самокритична («исторический PASS не доказательство»), но **содержит дрейф**: `implementation-v2/README.md` указывает «миграции до 00195», MVP-план — «цепочка до 2625», в репозитории нумерация до 02659, на проде применено 2597 (`deploy/framevela/README.md`).
* ≈1 350 упоминаний «NOT RUN» (≈300 — «tests authored NOT RUN») в `docs/implementation-v2/*.md`: значительная часть кода покрыта тестами, которые в записях процесса не запускались. Это не обязательно дефект кода, но **качество не доказано**.
* В репозитории документы с идентификаторами инфраструктуры (IP сервера, идентификаторы Cloudflare, логин SMTP-реле, адрес перенаправления почты) — `deploy/framevela/README.md`. Пароли не обнаружены, но такие сведения лучше хранить вне VCS.
