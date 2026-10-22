# Операционная приёмка WCAG 2.2 AA

Нормативная база: [WCAG 2.2, Recommendation](https://www.w3.org/TR/2024/REC-WCAG22-20241212/), интерпретация native/CLI — [WCAG2ICT](https://www.w3.org/TR/2025/NOTE-wcag2ict-22-20251211/). WCAG2ICT — информативная Group Note, не самостоятельный нормативный стандарт соответствия. Реестр содержит все55 действующих критериев A/AA (31A+24AA), каждый отдельно.4.1.1 удалён в2.2; AAA не включён в обязательный минимум.

- `wcag22-aa-criteria.json`:55ID, level, точная ссылка, метод и ожидаемое доказательство.
- `application-platform-registry.json`:16 application/platform profiles ×55criterion=880 стартовых NOT_TESTED. Это инвентарь проверки, не обещание поддержки всех ОС. Поддерживаемые платформы нужно явно утвердить перед выпуском; native mobile Photographer требует отдельного профиля, если он поставляется, а не молчаливого web PASS.
- `route-inventory.json`:41Photographer+3Portal=44 шаблона маршрутов. Это inventory, не доказанная применимость всех generic states. Конкретные nested loading/error/cancel/uncertain/denied/recovery проверять по достижимости; статическую страницу нельзя заставлять иметь искусственную ошибку/отмену.
- `native-screen-state-inventory.json`:Qt Studio15screens/states/dialogs, localCLI иStudio-agentCLI.
- `supporting-evidence-registry.json`:точные имеющиеся source/unit/одиночный desktop sample. Ни один из них не повышает полный критерий в PASS; несовпавшие текущие хеши видны явно.

## Что уже изменено в коде

Portal: читаемая типографика16px основной/14px вторичный, palette/focus, lang/locale, клавиатурно достижимый file input, lightbox focus/trap/restore/inert/fallbackname и перенос/scrollcontrols. Это source/component fixes, не browser/AT PASS. Старый PLAN.md описывает первоначальные проблемы и сохраняется как история. Прошлый desktop sample подтверждает только перечисленные computed styles/язык/фокус на одном2048px viewport; не контраст всех composite backgrounds, не320px reflow и не VoiceOver. Next/dependency изменения требуют нового build fingerprint.

Минимум16/14 — отдельная продуктовая политика, WCAG не задаёт общего абсолютного минимального размера шрифта. WCAG AA contrast:4.5:1 обычный текст,3:1 крупный; необходимая нетекстовая графика/границы3:1; targets24×24CSSpx или обоснованное исключение.44px — полезная продуктовая рекомендация, но не обязательный AA размер.

## Порядок

1. **P0 — воспроизводимость и область.** После текущего владельца зафиксировать exact source/build/deployment/browser/OS/AT версии, языки EN/RU/BG, темы, viewport. Использовать настоящие owned fixtures/API/auth; не canned responses. Подтвердить actual published admin/operator panels и реальные поставляемые OS/native targets. Не менять source snapshot активного владельца.
2. **P0 — клавиатура/имена/авторизация.** Portal landing→share→gallery selection→upload/download→lightbox; Photographer login→workspace→основной процесс и Settings; Qt list/YAML/preview/render/cancel/dialogs. Tab/ShiftTab/Enter/Space/arrows/Escape, без trap; имя/role/value, фокус после route/close/cancel/virtualization. Доступность не должна позволять защищённое действие после auth loss.
3. **P1 — шрифт/геометрия.** Реальный computed baseline всех читаемых текстов;200% text,400% zoom/320CSSpx, text-spacing override, portrait/landscape, high contrast/OS scale. Видимые focus indicators не скрываются sticky header/download panel. Существенная2Dфотография может требовать pan, но окружающие controls/instructions должны reflow.
4. **P1 — contrast и pointer.** Измерять actual composite colors каждого состояния/theme, включая hover/focus/error/selected, модальные панели. Targets/spacing и non-drag/single-pointer alternatives. Документировать только реально применимые исключения, а не blanket disabled/photo exemption.
5. **P1 — screenreader.** macOS VoiceOver+актуальные поддерживаемые Chrome/Safari; Windows NVDA+поддерживаемый browser если заявлен; Qt native accessibility bridge на заявленных ОС. Проверить headings/landmarks, label-in-name, lang частей, status/error/progress/uncertain announcements и отсутствие спама. Не запускать native automation параллельно текущему владельцу.
6. **P1 — полные процессы и unhappy states.** Photo import→edit/review/apply→export/delivery, recipient selection/download/upload, Settings invite/roles/CAS/recovery, local batch. Для каждого реально достижимого state — criterion-specific evidence. Отдельно OTP paste/autofill, expired session и восстановление без необоснованной потери введённого.
7. **P2 — media/help/content review.** Инвентаризировать реальные видео/аудио/анимации/help; только после этого назначать N/A соответствующим критериям. Не предполагать, что сервисная или CLI оболочка автоматически освобождает от accessibility review.
8. **Закрытие.** Применимые A/AA не имеют FAIL/NOT_TESTED; N/A содержит source/content/process rationale и reviewer evidence. PASS указывает exact page/state/platform/process и включает ручное наблюдение там, где автоматизации недостаточно. axe/DOM/unit/tsc отдельно не доказывают WCAG. Повторить затронутые проверки после изменения сборки; historical evidence не переносить автоматически.

## Evidence contract

Запись результата: `criterion_id, application, platform, supported_stack, source_vector, build_sha, route/screen, state, locale, theme, viewport/zoom, method, steps, expected, observed, status, evidence_refs, reviewer, checked_at`. PASS требует ссылки на фактические данные и rationale; N/A — конкретное обоснование применимости. Секреты/OTP/DSN не попадют в публичные артефакты. Неизвестный UI/недоступная ОС остаётся NOT_TESTED, а не N/A.

Проверка целостности новых реестров без запуска приложений: `.venv-platform/bin/python -B docs/implementation-v2/accessibility/validate-operational-registry.py` из repo. Это проверка структуры документации, не accessibility qualification.
