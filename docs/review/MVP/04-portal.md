# MVP-04. Портал клиентов (`apps/portal`)

Состояние: Next.js 16 (`output: "standalone"`), React 19; ≈1,2 тыс. строк; `tsc --noEmit` — 0 ошибок. Маршруты: `src/app/(entry)/page.tsx` (редирект на `/ru` с сохранением параметров локали — язык по умолчанию зашит, `Accept-Language` не учитывается), `src/app/(localized)/[locale]/page.tsx` (лендинг), `src/app/(localized)/[locale]/s/[share_id]/page.tsx` (выдача по ссылке). Основной компонент — `src/components/Portal.tsx` (сессия, галерея, выбор, скачивание, загрузка, lightbox). API-обёртка — `src/lib/api.ts`.

Портал в целом ближе всех к готовности (≈75–80 %): доработки — в безопасности заголовков, конфигурации окружения, декомпозиции и e2e.

| ID | Приоритет | Работа | Трудоёмкость |
|---|---|---|---|
| PT-1 | P1 | Убрать жёсткий домен и нормализовать метаданные | S |
| PT-2 | P1 | Заголовки безопасности (CSP, frame-ancestors, HSTS) | S |
| PT-3 | P1 | Идемпотентность `download-intent` при повторах | S |
| PT-4 | P1 | Декомпозиция `Portal.tsx` | M |
| PT-5 | P1 | Кэширование и производительность | S–M |
| PT-6 | P1 | Обработка `429`/`Retry-After` | S |
| PT-7 | P1 | e2e + доступность (axe) | M (см. [06](06-testing-ci-e2e.md)) |
| PT-8 | P2 | Конфигурация окружения во время выполнения, единый путь развёртывания | M |
| PT-9 | P2 | Единое имя продукта и тексты | S |

## PT-1. Жёсткий домен и метаданные (P1)

* **Где:** `src/components/LocaleLinks.tsx:10` — `useState('https://framevela.com'+path)`; `generateMetadata` в `src/app/(localized)/[locale]/s/[share_id]/page.tsx` (поля `alternates.canonical`, `alternates.languages`, `title:'Framevela'`) и в `src/app/(localized)/[locale]/page.tsx`.
* **Изменить:**
  1. `NEXT_PUBLIC_SITE_ORIGIN` (с запасным значением `''` → относительные ссылки); `LocaleLinks` строить из `window.location.origin` после монтирования либо использовать относительные `href` (`'/' + locale + path`).
  2. В `generateMetadata` задать `metadataBase: new URL(process.env.NEXT_PUBLIC_SITE_ORIGIN ?? …)`; для **share-страницы убрать** `alternates` (страница `noindex`, ссылка приватна — языковые альтернативы с идентификатором ссылки не нужны); оставить `robots: {index:false, follow:false}` и `referrer: 'no-referrer'`.
  3. Название бренда вынести в константу (PT-9).
* **Тесты:** `LocaleLinks.test.tsx` — ссылки не содержат прод-домен при пустом `NEXT_PUBLIC_SITE_ORIGIN`; метаданные share-страницы без `alternates`.

## PT-2. Заголовки безопасности (P1)

* **Где:** `apps/portal/next.config.ts` (`headers()`: сейчас только `Referrer-Policy`, `X-Content-Type-Options`, `Permissions-Policy`, `Cache-Control`); `deploy/caddy/Caddyfile` (блок `{$PORTAL_DOMAIN}` — `Referrer-Policy`, `X-Content-Type-Options`; блок `{$APP_DOMAIN}` — **без заголовков безопасности**); `deploy/framevela/Caddyfile`.
* **Добавить:**
  * `Content-Security-Policy`: `default-src 'self'; img-src 'self' data: blob: https://<storage>; media-src 'self' blob: https://<storage>; connect-src 'self' https://<api> https://<storage>; script-src 'self' 'nonce-…'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'` — домены из `NEXT_PUBLIC_API_ORIGIN`/`NEXT_PUBLIC_STORAGE_ORIGIN`; для Next использовать nonce через `middleware.ts`;
  * `X-Frame-Options: DENY` (совместимость), `Cross-Origin-Opener-Policy: same-origin`, `Strict-Transport-Security: max-age=31536000; includeSubDomains` (на уровне Caddy/Cloudflare), `X-Content-Type-Options` (есть);
  * то же для приложения фотографа (`{$APP_DOMAIN}` в Caddyfile).
* **Проверка:** e2e-тест, что страница не встраивается в `<iframe>`, и `securityheaders`-подобный smoke в CI (`scripts/check-headers.mjs`).

## PT-3. Идемпотентность `download-intent` (P1)

* **Где:** `src/lib/api.ts` — `redeemDownload(csrf, id, volume, …)` и `downloadItem(csrf, itemID, presetID, …)` вызывают `idempotencyKey()` **внутри** функции при каждом обращении; для `startDownload/createSelection` ключ передаётся параметром (хорошо).
* **Изменить:** ключ формировать вызывающим кодом и хранить в `sessionStorage` вместе с задачей (`src/lib/download-recovery.ts`, `download-preparation.ts`), чтобы повтор после обрыва сети использовал тот же ключ; параметр `key = idempotencyKey()` по аналогии с `startDownload`.
* **Тесты:** `src/lib/api.test.ts`/`download-recovery.test.ts` — повтор с тем же ключом отправляет тот же `Idempotency-Key`; другая пара `(id, volume)` — другой.

## PT-4. Декомпозиция `Portal.tsx` (P1)

* **Проблема:** один компонент (350 «строк», 29 `useState/useRef`, 5 `AbortController`) смешивает сессию, галерею, виртуализацию, медиа-ссылки, выбор, скачивание, загрузку и lightbox.
* **Структура после:**
  ```
  src/components/Portal.tsx            // композиция и разметка
  src/features/session/usePortalSession.ts   // exchange, csrf, epoch/authority, refuseDownloadAccess
  src/features/gallery/useGallery.ts         // items, cursor, virtualWindow, фокус заголовка
  src/features/gallery/useMediaLinks.ts      // MediaCache, fetchMedia (таймаут 8 с)
  src/features/selection/useSelection.ts
  src/features/download/useDownloadJob.ts   // prepare/poll/cancel/redeem, lib/download-*.ts
  src/features/upload/useGuestUpload.ts     // uploadRecoverably, lib/upload-recovery.ts
  src/lib/useAbortScope.ts                  // общий владелец AbortController-ов
  ```
* **Правила:** явная машина состояний сессии `loading | ready | invalid | error` (`useReducer`); никаких `ref`-флагов «авторитета» в компоненте — только внутри `usePortalSession`; поведение `refuseDownloadAccess` (сброс всего при `401/403/404/410`) сохранить и покрыть тестом.
* **Тесты:** компонентные (Testing Library + jsdom) для каждого хука; текущие `lib/*.test.ts` — без изменений.

## PT-5. Кэширование и производительность (P1)

* **Где:** `next.config.ts` — `Cache-Control: no-store` для `/:path*` (применяется ко всем путям, включая лендинг, иконки, статику из `public/`); `src/lib/media-cache.ts` (LRU на 512 ссылок, ссылка отбрасывается за 30 с до истечения); `src/lib/virtual-grid.ts`.
* **Изменить:**
  1. `Cache-Control: no-store` оставить только для `/:locale/s/:share_id` и ответов API; лендинг и статику — `public, max-age=…, stale-while-revalidate` (проверить фактическое поведение в production-сборке: Next переопределяет заголовки для `/_next/static`, но не для `public/`).
  2. Для `<img>`: в сетке уже заданы `width/height` и `loading="lazy"` (`Portal.tsx`); добавить `decoding="async"`, `fetchpriority="high"` для первых строк, размеры и `decoding` для изображения lightbox (сейчас без них), предзагрузку следующего окна по `virtualWindow`.
  3. Подписанные ссылки хранилища: `Cache-Control: private, max-age=<ttl-60>` на стороне MinIO/gateway, чтобы браузер не перекачивал миниатюры.
  4. Нагрузочный профиль: галерея 2 000–5 000 элементов, 3G/«Fast 3G» throttling; бюджет — первое изображение ≤ 2,5 с (LCP), без long task > 100 мс при прокрутке.
* **Файлы тестов:** `lib/virtual-grid.test.ts` (расширить), e2e Lighthouse CI.

## PT-6. `429` и `Retry-After` (P1)

* **Где:** `src/lib/api.ts` — `decode()` и класс `APIError`.
* **Изменить:** `APIError` получает `retryAfterSeconds?: number` (из заголовка `Retry-After`); `Portal.tsx`/хуки показывают сообщение `t.rate_limited` с обратным отсчётом и блокируют повтор до истечения; ключ локализации — `src/catalogs/{en,ru,bg}.json`. Серверная часть — [02 B-1](02-backend-and-contracts.md).

## PT-7. e2e и доступность (P1)

* Playwright-сценарии (подробно в [06](06-testing-ci-e2e.md)): вход по ссылке с секретом во фрагменте; невалидный/отозванный/просроченный секрет; просмотр, lightbox с клавиатуры (фокус-ловушка, Esc); выбор «все/выбранные»; скачивание файла и архива (разбиение на тома); отмена; повтор после обрыва сети; гостевая загрузка с возобновлением; 3 локали; axe-проверка контраста и меток.

## PT-8. Окружение и путь развёртывания (P2)

* **Проблема:** `NEXT_PUBLIC_API_ORIGIN` (`src/lib/api.ts: base()`) вшивается при сборке — образ привязан к окружению; существуют два пути развёртывания: контейнер `deploy/images/portal` (compose, `output: "standalone"`) и Cloudflare Worker (`deploy/framevela/portal/{portal-worker.mjs,vite.config.ts,wrangler.jsonc}`).
* **Изменить:** (а) конфигурация во время выполнения — серверный компонент читает `process.env.API_ORIGIN` и передаёт в клиент через `<script id="runtime-config" type="application/json">`/props; (б) выбрать один путь для MVP: для «небольших серверов в кластере» — контейнер за Caddy; Cloudflare оставить как опцию CDN перед Caddy; (в) удалить из `deploy/framevela/portal` дубли сборки или оформить как явный альтернативный профиль.

## PT-9. Имя продукта и тексты (P2)

* Единая константа `BRAND` (`src/lib/brand.ts`) и ключи в `src/catalogs/*.json`, `src/lib/site-i18n.ts`; заменить `'Framevela'` в `generateMetadata` и `LocaleLinks`; согласовать с именами пакетов `@photoeditor/*`.
