# Ревью UI `apps/photographer`: соответствие React Native Paper и качество интерфейса

Дата: 2026-10-23. Метод: статический разбор AST всех производственных TS/TSX в `app/`, `screens/`, `src/` (585 файлов, без `*.test.*`) сканером [`scan_paper.mjs`](scan_paper.mjs) плюс чтение выборки экранов и контракта [`photographer-paper-ui-contract.md`](../implementation-v2/photographer-paper-ui-contract.md). **Код приложения не менялся**; план исправления — [PLAN.md](PLAN.md). Ограничение: ревью статическое — приложение не запускалось, контраст и вёрстка не измерялись (это пункт UI-14 плана).

Повторить: `cd apps/photographer && node ../../docs/ui-audit/scan_paper.mjs > /tmp/paper-scan.json`.

## 1. Итог

| Вопрос | Вердикт |
|---|---|
| Остались ли запрещённые примитивы React Native (`Text`, `TextInput`, `Button`, `Switch`, `Modal`, `Pressable`, `Touchable*`, `ActivityIndicator`)? | **Нет** — ни прямым импортом, ни через пространство имён, `require` или `Animated.Text`; ни в `app/`, ни в `screens/`, ни в `src/`. `Alert`, DOM-теги и сторонние UI-библиотеки (кроме иконок Paper) не используются; пользовательские строки в JSX не захардкожены. |
| «Paper везде» выполнено в полном смысле? | **Нет.** Выполнено на уровне импортов. По сути — частично: тема не применяется, стили хардкодят цвета и шрифты, часть кнопок и списков сделана вручную поверх `TouchableRipple`/`View`, словарь Paper узкий (7 компонентов). |
| Что подтверждает политика-тест? | Меньше, чем сказано в контракте: он сканирует только `app/` и `src/`, а основной код экранов лежит в `screens/`; проверяет только именованный импорт из `react-native`. |

## 2. Найденные отклонения

Нумерация F-nn связана с пунктами [PLAN.md](PLAN.md) (UI-nn).

**F-01. Политика-тест не покрывает `screens/` и обходится.** `src/paper-ui-policy.test.ts` читает `app` и `src`; контракт обещает «все production TS/TSX в app/src», хотя в `screens/` лежат 42 экрана (≈ 390 КБ, треть UI-кода). Сегодня нарушений там нет (сканер: 0), то есть риск — на будущее, включая 12 новых видео-экранов `screens/video/…`. Тест не ловит `import * as RN`, `require('react-native')`, `Animated.Text`, сторонние UI-библиотеки, `Alert`. → UI-01.

**F-02. Приложение только тёмное; светлая тема не подключена.** `photographerLightTheme` определена в `src/paper-theme.ts`, но нигде не используется; `PhotographerPaperProvider` всегда передаёт `photographerDarkTheme`, `app/_layout.tsx` задаёт фон также из неё. Контракт признаёт: «Внедрение Paper не является доказательством применения всех сохранённых theme preferences». Спецификация видео-редактора ([23](../video/23-editor-playback-timeline.md), D-36, §20.4) требует обе темы. → UI-02.

**F-03. Тема не читается — цвета зашиты в модульные `StyleSheet`.** Из 206 файлов с разметкой `useTheme` встречается в одном (`src/navigation-return-links.tsx`). Остальные берут цвета статически (`const {colors}=photographerDarkTheme` в `src/catalog-style.ts`, который импортируют 74 файла), поэтому смена темы на лету невозможна. → UI-03 (предпосылка UI-02).

**F-04. Жёсткие цвета, дублирующие роли темы или мимо неё.** 75 вхождений в 29 файлах (сканер, правило `hardcoded-color`):
- `screens/login.tsx`, `screens/workspaces.tsx`: фон `#222`, карточки `#303030`, кнопки/ошибки `#d0bcff`, `#ffb4ab`, `#201a2b`, текст `white`, `#ddd`, `#bbb`, `#888` — буквальные копии ролей MD3 Dark, при светлой теме нечитаемы;
- `screens/collections/[id]/index.tsx`: кнопки «принять/отклонить» `#287a45`/`#8b3434`, ярлыки `#454545` — самодельные кнопки статуса;
- `screens/runs/[id].tsx`: прогресс `#555`/`#d0bcff`;
- `src/auth-context.tsx` (`#222`) и `src/read-resource-state.tsx` (`#eeeeee`) — состояния загрузки и ошибки, видимые на любом экране;
- `placeholderTextColor="#888"` в `exports/index.tsx`, `collections/[id]/apply.tsx`, `settings/{members,profile,retention}.tsx`, `styles/[id]/training.tsx` — Paper сам красит подсказку;
- холсты и просмотры (`mask-canvas`, `perspective-*`, `compare-*`, `region-loupe`, `*-preview-view`, `interchange-*`) — тёмная подложка `#202124` и цвета маркеров: допустимы как «специализированные поверхности» контракта, но должны быть именованными токенами, а не литералами. → UI-04, UI-05.

**F-05. Типографика мимо шкалы MD3.** 770 из 787 `<Text>` без `variant`; размер и насыщенность задаются стилями (30 вхождений `fontSize|fontWeight|lineHeight`: `catalog-style.ts` — 7, `login.tsx` — 5, `workspaces.tsx` — 6, `import-color-profile.tsx` — 5 и др.; заголовок экрана `workspaces.tsx`: `fontSize:28, fontWeight:"700", color:"white"`). Читаемые шрифты уже централизованы (`paper-readable-fonts.ts`), но экраны их не используют. → UI-06.

**F-06. Самодельные «Paper-подобные» элементы.**
- `catalogStyle.button`: фон `colors.primary`, радиус 8, свой цвет текста (`buttonText`) — контракт прямо запрещает: «старые фоны/цвета catalog-кнопок не должны перекрывать disabled цвета Paper». Используется как `style` у Paper `Button` (редактор: `screens/editor/[version].tsx`) и у `TouchableRipple` (`screens/sessions/[id]/index.tsx`).
- 77 `TouchableRipple`, из них 27 с `accessibilityRole="button"` собраны в кнопки вручную (лидеры: `screens/editor/[version].tsx`, `src/capture-session-create.tsx`, `src/interchange-job-screen.tsx`, `src/interchange-prepared-review-screen.tsx`).
- Строки списков (`workspaces`, `projects`, `projects/[id]`, `exports`, `styles`, `reviews`, …, 9 файлов) — `View` + `TouchableRipple` + текстовый глиф «›» вместо `List.Item`/`Card`.
- Прогресс задания (`screens/runs/[id].tsx`) — два `View` вместо `ProgressBar`.
→ UI-07, UI-08.

**F-07. Узкий словарь Paper.** В коде используются `Text` (787), `Button` (574), `TouchableRipple` (77), `TextInput` (64), `ActivityIndicator` (60), `Card` (10), `RadioButton` (2). Нет `Appbar` (заголовки экранов — `Text` внутри собственной шапки; `headerShown:false`), `List`, `Chip`, `Switch`, `Checkbox`, `Dialog`/`Portal`, `Snackbar`, `Banner`, `ProgressBar`, `Divider`, `SegmentedButtons`, `Menu`. Спецификация видео-экранов ([03](../video/03-photographer-screens.md)) требует `ProgressBar`, `Banner`, `Switch`, `Modal`/диалоги — общей обёртки для них нет, и слабая модель сделает каждый экран по-своему. → UI-09.

**F-08. `SafeAreaView` из `react-native` в 42 файлах.** В React Native 0.86 компонент объявлен устаревшим; приложение уже зависит от `react-native-safe-area-context` (`SafeAreaProvider` в провайдере). → UI-10.

**F-09. Доступность полей.** `screens/collections/[id]/lens.tsx` — Paper `TextInput` (десятичная сила) без `label`; `screens/exports/index.tsx` — только `placeholder`. Подсказка не заменяет подпись. → UI-11.

**F-10. Единичные несоответствия контракту.** В `accessibilityRole="button"` на Paper `Button` (избыточно; Paper задаёт роль сам) — массово, безвредно; не включено в план.

## 3. Что в порядке

- 0 запрещённых примитивов в 585 файлах; 0 `Alert`; 0 DOM-тегов вне web-файлов; 0 жёстко заданных строк в JSX (все через `t()`/`__uiT`).
- Единый `PhotographerPaperProvider` с `SafeAreaProvider` и иконками; состояния загрузки/ошибки через `ReadResourceState` на Paper.
- Минимальные размеры касания вручную задаются (`contentStyle={{minHeight:48}}`), у `TouchableRipple` и кнопок есть `accessibilityLabel`/`accessibilityState`.

## 4. Связь с видео-ТЗ

Новые экраны V-01…V-14 по [03](../video/03-photographer-screens.md) наследуют все перечисленные проблемы, если взять существующие экраны за образец (а карточки на них ссылаются). Поэтому порядок исправлений: UI-01…UI-03 и UI-09 (политика, тема, общий набор) — **до** карточек TK-6xx; в [`docs/video/15-task-cards.md`](../video/15-task-cards.md) добавлена зависимость.
