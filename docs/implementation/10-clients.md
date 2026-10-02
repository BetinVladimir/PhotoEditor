# 10. Expo Paper и Next.js MUI

## Общие модули

`packages/api-client` generated OpenAPI types + transport. `ApiClient.request<T>` attaches access bearer native/web owner, credentials same-origin cookies для guest/refresh, CSRF mutators, request_id; retries только GET/idempotent same-key POST при network unknown outcome max2. 401→single-flight refresh, повтор один раз; 403/409/422 не retry. Logout clears cache/SecureStore/current requests.

`QueryKeys.tenant(t,resource,filters)` includes tenant+principal, guest keys share_id+session_epoch. Workspace switch cancel queries, clear tenant-scoped Zustand draft selection, invalidate old results, resume own new event cursor. `EventPoller.start(scope,cursor,onEvent)`, `stop`, `recover`: poll2s active,5s idle-visible, backoff до30s hidden/offline, jitter; EVENT_CURSOR_EXPIRED→refetch snapshot. Event только invalidate/read authoritative data, не локально применять непроверенные mutators.

`UploadQueue` методы `enqueue(files,scope)`, `resume(upload_id,file)`, `sendPart`, `refreshURLs`, `complete`, `abort`, `restore`. Persistence local metadata IDs/name/size/lastModified/hash, не secrets/signed URLs. Web File handles нельзя гарантированно восстановить после reload: попросить повторно выбрать тот же файл и сверить hash. Native file permission проверять после restart. Part concurrency2/file, max2files, bounded memory one part per slot; offline pause, не server cancel.

`SelectionStore` explicit IDs либо snapshot_id, collection_revision; range selection только known ordered positions, all через server snapshot. Filter/tenant change clears select-all, не добавляет новые items в frozen selection. Server authoritative count в confirmation. Theme/locale change selection/upload не теряются.

## Expo routes и компоненты

| Route | Компоненты / методы / API / acceptance |
|---|---|
| `(auth)/login` | EmailForm.submit→OTPRequest; OTPForm.verify/resend; autofill/paste; одинаковые ошибки; язык/тема доступны до login |
| `(app)/workspaces` | WorkspacePicker.select/create; Tenant list; clear tenant caches, owner bootstrap |
| `projects/index`, `[id]` | ProjectGrid.search/filter/create, ACL editor; responsive list, empty/offline |
| `collections/[id]` | VirtualizedPhotoGrid, SectionsNav, SelectionToolbar, PublishPanel, ShareLinksPanel; infinite queries cursor, no original image grid |
| `imports/new` | FileDropZone(web)/DocumentPhotoPicker(native), UploadQueuePanel, per-file errors/resume |
| `styles/index`, `[id]` | ProfileCard/VersionList; rename/archive/export/import, metrics+confidence distinct |
| `styles/[id]/training` | PairWizard: name→upload columns→confirm pairs→analyze→comparison→save; PairCard.swap/exclude/replace; no unconfirmed analyze |
| `collections/[id]/apply` | ApplyWizard select explicit version→scope→strength/overrides→preview→output_name→start; show new collection and selected count |
| `editor/[version]` | EditorShell/Filmstrip/CompareCanvas/ToolPanels/MaskCanvas; draft save/preview/manual SaveEdits→new run+collection |
| `reviews/index`, `[id]` | ReviewCard reasons+before/candidate; accept/reject/skip exact hash/revision; batch explicit IDs |
| `runs/[id]` | RunProgress, FailedItems, cancel/retry; show output draft partial status |
| `exports/index` | PresetEditor, DownloadJobCard, volume buttons; native save/share |
| `settings/*` | ProfilePreferences, Members, Quotas, Retention; sysadmin SMTP separate restricted surface |

`EditorStore` typed `{versionId,sourceSha,draftRevision,params,maskRefs,history,historyIndex,selectionObjectId,viewport}`. `applyCommand(command)` increments revision and pushes params/mask refs, no pixel copies. `undo/redo` restores state+increments monotonic request revision. `requestPreview` debounce200ms, cancel old client subscription, submit with draftRevision; old server task may finish but `acceptPreview(response)` compares exact revision+source hash. Preview task must not generate diffusion on slider changes; Generate button explicit invokes generation candidate.

`Viewport.screenToSource(point)` inverse current display transform; pan/zoom shared before/after, normalized oriented source storage. Mask brushes/polygon/positive/negative points/protect required on web/native. 100% ROI requests full-resolution engine context, global frozen params unchanged. Proxy marked approx; max zoom не превращает proxy в detail source. Theme neutral gray canvas fixed; photo pixels unchanged by dark mode.

Panels: Color curves/gains/LUT strength; Beauty skin/tone/texture; Defects candidate list; Body face/waist/hip/torso/leg with confidence; Mask selector/brush; Quality denoise/upscale/dust; Background alpha/solid/asset/blur/sky; Generation prompts/mask/seed/variants/accept; Composition aspect/headroom/horizon/manual lines; HDR explicit bracket/exposure/reference/window mask; Export presets. Each panel maps directly typed style/job/override fields; server capabilities disable unavailable model with reason, не скрывают обязательную недоставленную функцию.

## Portal routes и компоненты

`app/[locale]/s/[share_id]/page.tsx` renders public shell only; protected data client fetch Go same-origin `/api/v1`. Unsupported locale→en; next/font/local cyrillic/latin. No protected SSR props containing fragment/secret. Next Full Route/Data cache disabled for protected routes, `fetch cache:no-store`, no shared CDN/service-worker storage.

| Компонент / функция | Поведение |
|---|---|
| `CapabilityBootstrap.exchangeFragment()` | reads fragment, POST session, replaceState после success; no logging; expired/revoked same external screen |
| `GalleryHeader` | cover/name/description safe text, photographer branding optional own assets; no arbitrary HTML |
| `GalleryGrid` | React Photo Album rows/justified; cursor pages100; render window ≤300 items + overscan, reserve aspect dimensions; load thumb640/320 |
| `GalleryLightbox` | lazy client import YARL Zoom/Thumbnails/Fullscreen/Counter/Captions; preload±1, keyboard/touch/Escape; download own API button |
| `MediaLinkManager.get/refresh` | batch≤100 permitted IDs, cache in memory until expiry-10s; URL403/expiry refresh once, then error/manual retry |
| `SelectionBar` | explicit count, select all via server snapshot; restores scroll anchor on close, local storage only nonsecret preference |
| `UploadPanel` | upload-only own file queue/status; pending_review doesn't expose photos of others; moderation status from Go |
| `DownloadDialog` | allowed preset selection, job progress volumes, ready button; no thumbnail download substitution |
| `PortalStateBoundary` | opening/empty/no_view/expired/uploading/verifying/archive_preparing/ready/error; no internal task IDs |

View/download/upload независимы. view-only получает previews и no download controls; upload-only не запрашивает `/portal/items`; download-only имеет blind all-download CTA без каталога. Link permissions change invalidates session with retry-open-link state. Cookie сохраняет session при очистке fragment, reopening original URL creates fresh session. Password/OTP гостю не показывать.

## Локализация и доступность

ru/bg/en, preference user→local→device→en, locale normalize region. Shared namespaces common/errors/downloads/uploads; Expo auth/projects/editor/jobs/settings/app; Portal portal. `I18nService.setLocale` updates stores/server profile, не remount app. Intl date/number/plural, API UTC. Titles/user prompts не переводить. Translation catalog checker exact key equality и no raw keys; ручная редактура RU/BG обязательна перед release.

ThemeProvider Paper MD3 only Expo; MUI+Emotion only portal, общие semantic tokens light/dark/system. Motion150–250ms opacity/transform, prefers-reduced-motion turns movement off. Focus traps/return, labels, keyboard, screenreader, large text, touch targets>=44px, color+text statuses. Web/native same feature coverage; drag-drop не единственный upload path.

## Frontend тесты

React unit tests state machines (preview revision, selection snapshots, upload resume), generated DTO contract fixtures, Playwright RU/BG/EN×light/dark×desktop/mobileweb, Maestro iOS/Android core flows. 5000-item gallery DOM bounded, thumbnails only network test. Offline run survives; refresh reuse singleflight; wrong tenant cache cleared; revoked link no new URL; accept wrong candidate revision409 keeps review screen. UI не доказывает GPU/algorithm quality.


Apply v4 editor controls are enabled only for branches compiled from the persisted OverrideV1 overlay. Beauty, reviewed defects, safe geometry and restoration enter identity SaveEdits planning and runtime admission. Sensor dust accepts only explicit review policy; the review detail endpoint returns content-verified candidate IDs, and the native/web screen lets the editor choose the accepted subset while binding the decision to revision and candidate hash. Automatic dust approval remains unavailable. The typed composition panel persists aspect, headroom and bounded rotation, and enables outpaint only through an explicit `review` policy with prompt, seed and guidance; operation availability gates structure, planning, crop and outpaint independently. The typed background panel persists keep, transparent, bounded solid RGB and depth blur modes, always protects the subject, and gates compose and blur capabilities separately. The client continues to gate unavailable model operations from `/capabilities` and cannot silently persist an unsupported selection.

Экран коллекции создаёт контактный лист только из новой immutable selection snapshot. Кнопка блокируется для пустой коллекции, открывает общий экран run progress и сообщает отдельно о неготовых previews или media capability. Итоговые PNG pages и JSON manifest выдаются тем же безопасным механизмом run outputs.
