# WCAG 2.2 AA — все приложения PhotoEditor

Статус: исходный план, никакой UI/runtime conformance не подтверждён. Новое обязательное требование пользователя охватывает весь продукт, а не только Portal. Текущие frozen evidence и активная R51 не изменены. Нормативный источник: [W3C WCAG 2.2](https://www.w3.org/TR/WCAG22/). Для native desktop использовать [WCAG2ICT](https://www.w3.org/TR/wcag2ict/) как интерпретацию требований, плюс платформенное accessibility API; не выдавать web axe за desktop-аудит.

## Область и пользователи

- Photographer: Expo Router / React Native Web / React Native Paper. Фотограф, студия, owner/admin/editor/viewer.41 файлов маршрутов, все перечислены в route-inventory.json. Layout — отдельная общая граница, не маршрут.41+3Portal=44, не44Photographer.
- Portal: Next.js/React, публичный лендинг `/`, локализованный `/[locale]`, клиентская галерея `/[locale]/s/[share_id]`. Получатель фотографий: просмотр, выбор, загрузка/скачивание, прогресс, ошибки/отказ/истечение доступа.
- Local editor: PySide6 desktop Studio, CLI. Главное окно: список файлов, Before/After, YAML, журнал, Validate/Save/Preview/Render/Cancel. Диалоги invalid/busy; processing/completed/stale revision/cancel/close while running. Пользователь локального пакетного редактора.
- Studio-agent: Python CLI watch-folder, status/retry/run, keychain/config errors; GUI в исходниках приложения не найден. Проверять понятный текст и status/error channels, терминал/VoiceOver, отсутствие информации только цветом, пользовательский масштаб терминала. Не объявлять headless service WCAG web page.
- Admin: backend/internal/admin — API/service; самостоятельный admin frontend не обнаружен в apps. Пользовательские админ-поверхности уже входят в Settings/Capture Photographer. Все фактически опубликованные внешние панели из deploy отдельно включить в runtime surface inventory; отсутствие собственной админ-страницы не является PASS сторонних панелей.

## Подтверждённые исходные проблемы и риск

|Приоритет|Источник|Факт и действие|Критерий/граница доказательства|
|---|---|---|---|
|P0|Portal.tsx:335|Upload — label с hidden input, без keyboard-focusable trigger. Заменить доступной кнопкой/визуально скрытым, но фокусируемым input; проверить Enter/Space и доступное имя.|2.1.1/4.1.2; source defect, фактический browser ещё UNRUN|
|P0|Portal.tsx:342 и effects204–206|Lightbox aria-modal есть; focus entry/trap/restore/Escape в текущем компоненте не реализованы. Добавить реальное управление фокусом и inert background, доступное имя при пустом title.|2.4.3/2.1.1/4.1.2; отсутствие Esc само по себе не универсальный WCAG fail, обязательна работоспособная клавиатурная Close|
|P1|landing.css|Навигация13px, primary CTA12px, eyebrow/hero note10px, art labels8/6px. Поднять все читаемые пользовательские подписи к baseline; декоративные надписи сделать действительно декоративными, не прятать необходимую информацию.|Новый продуктовый минимум16/14, не выдуманный WCAG минимальный размер|
|P1|global.css|eyebrow .75rem, mobile tile label .85rem. При base16 это12/13.6px; поднять вторичный текст≥14px. Проверить реальные computed styles всех breakpoints.|Продуктовый baseline;1.4.4/1.4.10 проверяются отдельно|
|P1|local-editor studio.py|Нет explicit accessibleName для YAML/log/file list/BeforeAfter; pixmap заменяет текст QLabel, нет текстовой альтернативы пути/статуса изображения. Добавить accessibleName/Description, labels/buddies, meaningful status events.|1.1.1/1.3.1/4.1.2/4.1.3 native interpretation; platform test обязателен|
|P1|photographer paper-theme.ts|Расширены цвета MD3, typography остается библиотечной. Paper сам по себе не гарантирует размер14/16 и AA. Зафиксировать типографические roles и проверить каждый потребитель, оба theme.|Нет заявления о подтверждённом контрастном дефекте без computed palette/state measurement|
|P1|Общие route/state contracts|Нынешние source/unit/browser cases функциональные; не дают полный WCAG evidence, desktop AT и zoom не покрыты.|Недостающее принятие, не автоматически дефект каждого экрана|

## Типографический контракт

Основной читаемый текст≥16CSSpx, вторичный≥14CSSpx, UI inputs≥16CSSpx. Относительные rem/масштабируемые native units, line-height по контексту, без фиксированной высоты, обрезающей текст. Пользовательский zoom/Dynamic Type не отключать. Это отдельное продуктовое требование: WCAG не предписывает абсолютный16px минимум. Не увеличивать только body — проверить CTA, подписи, ошибки, chips, меню, таблицы, dialogs, footer, mobile. Desktop системный шрифт масштабируется по OS settings; нельзя считать CSSpx прямой единицей Qt.

## Полный acceptance mapping

Каждый критерий уровня A и AA имеет запись PASS/FAIL/NOT_APPLICABLE с доказательством. N/A — только по настоящему содержимому/достижимости; не по отсутствию axe violation. AAA не является требуемым минимумом. Для каждого из44маршрутов и native screens пройти все применимые состояния inventory, включая вложенные формы/dialogs/виртуализацию и полные процессы.

|Группа критериев A/AA|Автоматизация/исходники|Обязательное наблюдение человеком|
|---|---|---|
|1.1.1|Имена img/buttons, SVG/canvas, контентные фото и ошибки загрузки|Смысловые альтернативы/BeforeAfter; редактирование изображения доступно без выдуманного описания результата|
|1.2.1–1.2.5|Инвентарь фактического аудио/видео, captions/transcript tracks|Если медиа существует — точность captions/audio description; сейчас N/A не назначать без content inventory|
|1.3.1–1.3.5|DOM roles/headings/forms/labels/autocomplete; native accessibility tree|Порядок чтения, связь ошибок/полей, ориентация portrait/landscape, смысл не только положение/цвет|
|1.4.1–1.4.5|Контраст normal text≥4.5:1, large≥3:1; нет отключения zoom; media sound controls|200% text, цветовые статусы с текстом, исключения logo/incidental/disabled документируются|
|1.4.10–1.4.13|320CSSpx reflow/400% browser zoom от1280; control outlines≥3:1; text-spacing override|Нет потери/двумерного scroll кроме реальных допускаемых2Dimage regions; menus/tooltips dismissible/hoverable/persistent; letter/word/paragraph spacing без обрезки|
|2.1.1/2.1.2/2.1.4|Keyboard sequences, запрет случайного single-key shortcut capture|Весь процесс Tab/ShiftTab/Enter/Space/arrows/Escape; нет trap, альтернативы canvas/drag|
|2.2.1/2.2.2|Auth/session/read/write timeout policies, motion controls|Время/продление с security exceptions обоснованы; не терять введённое/журнал без объяснения, pause animations|
|2.3.1|Запрет flash patterns в коде/медиа inventory|Нет опасного мигания; фото-контент не считать автоматически доступным|
|2.4.1–2.4.7/2.4.11|Titles, bypass links, labels/link names, focus CSS|Логичный focus order/visible/not entirely obscured, route transitions/modal close/virtual-list restore; multiple ways для применимых страниц|
|2.5.1–2.5.4/2.5.7/2.5.8|Target24×24CSSpx или допустимое spacing/исключение; label-in-name|Одноточечная/безdrag альтернатива, pointer cancellation, motion alternative, реальные touch targets;44px продуктовая рекомендация, не AA минимум|
|3.1.1/3.1.2|lang документа/частей EN/RU/BG, native locale|Правильное произношение screenreader после переключения|
|3.2.1–3.2.4/3.2.6|Стабильные names/nav/help anchors|Фокус/ввод не вызывает неожиданную смену контекста; help consistent если он есть|
|3.3.1–3.3.4/3.3.7/3.3.8|Labels/errors/validation/redundant input/auth forms|Ошибки объявляются и исправимы, confirmation/reversal опасных действий; OTP допускает paste/autofill, без cognitive puzzle/запрета password manager|
|4.1.2/4.1.3|axe+role/name/value/live region checks|VoiceOver корректно сообщает loading/progress/error/cancel/uncertain, не спамит повторениями; статус не только visual|

1.4.6–1.4.9,2.4.8–2.4.10/12/13 и другие AAA не выдавать за обязательное AA.4.1.1 в WCAG2.2 удалён; DOM validity всё равно полезная инженерная проверка.

## Реальный порядок реализации и проверки

1. После terminal/source snapshot R51 root выделяет новый production change. Сначала общий typography baseline и Portal upload/lightbox, затем Photographer common Paper labels/focus/status и native accessible names; не править pinned live source во время владельца.
2. Создать отдельный criteria registry с page/state/locale/theme/viewport/criterion/expected/evidence/status; route-inventory — каркас, а не закрытие state applicability. Привязать все реальные nested controls к source functions. Пользовательские admin actions также обязательны.
3. DOM/unit regression: accessible name и label-in-name, error associations, focus return, immutable state при zoom/locale, read button lifecycle. Искусственные doubles подтверждают только компонент, не приложение.
4. На настоящем future build: axe на каждом достижимом состоянии +Playwright keyboard/200%/400%/320px/text-spacing/contrast computed checks. Сохранить exact build/source/browser/tool versions, fixture IDs без секретов, скриншоты и failure traces. Paper mapping на DOM проверять фактически.
5. Ручная приёмка: macOS Chrome/Safari+VoiceOver, keyboard only; Windows NVDA+поддерживаемый browser перед объявлением Windows поддержки; Qt/macOS VoiceOver и поддерживаемые desktop OS AT. Точный набор supported platforms зафиксировать; недоступная платформа UNRUN.
6. Полные процессы: auth→workspace→import→editor→review/apply→export/delivery, клиент share→select→download/upload, settings role/invite/CAS/recovery и локальный preview/render/cancel. Нельзя принять только отдельные routes, если процесс ломается.
7. Release gate: ноль неразрешённых применимых A/AA FAIL, no UNKNOWN/UNRUN для заявленной платформы; все N/A обоснованы. Отдельный user-readable accessibility statement с известными ограничениями. axe-only PASS не равен WCAG conformance.

## Граница текущего результата

Источник проинвентаризирован, конкретные Portal/native проблемы показаны. Ни один из44routes не объявлен WCAG PASS. Контраст, размеры rendered targets, реальные focus/AT и полные state matrices ещё требуют текущей сборки. Ничего не перемещено и не изменено в app/frozen/evidence/cache.


## Root follow-up: 2026-10-20

После снимка исправлены исходники Portal: landing typography/contrast/navigation wrap; global eyebrow/mobile caption14px, более контрастный focus, file input визуально скрыт вместо hidden (сохраняет native keyboard focus, label name, disabled), label focus-within; root defaultlangru и explicitlocale для обоих состоянийPortal/localehome. Это SOURCE_CHANGED, не runtimePASS и не productiondeployment. Исходные pins выше исторические; повторная browser/axe/keyboard/currentcomputedstyles и обновлённые pins обязательны. Lightboxfocus и прочие44route/state/native проверки остаются OPEN.
