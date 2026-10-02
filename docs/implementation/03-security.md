# 03. Авторизация, права и безопасность

## AuthService

| Метод | Вход → выход; действия и ошибки |
|---|---|
| `RequestOTP(ctx,Email,Locale,ClientMeta) ChallengeAccepted` | trim+Unicode normalize email; domain IDNA/lowercase; local-part lowercase как продуктовая identity policy; length<=254. Crypto rand uniform 000000..999999; HMAC(pepper,challengeID\|email\|code); TTL300s, resend60s; всегда 202 с challenge_id, включая unknown/disabled registration. |
| `VerifyOTP(ctx,ChallengeID,Code,ClientType) TokenPair` | auth tx FOR UPDATE; expiry/attempts/consumed; constant-time HMAC; failed attempts++; success consumed once, user+personal tenant при enabled registration, session; одна транзакция через trusted bootstrap fn |
| `RotateRefresh(ctx,Token,CSRF) TokenPair` | hash token; lock family; active token→used, новый random256 token; повтор used token отзывает family, 401; concurrent client refresh запрещён single-flight клиентом |
| `AuthenticateAccess(ctx,JWT) Principal` | Ed25519 alg allowlist, issuer/audience/exp/nbf, sid; проверить session alive + user active; claims не дают membership |
| `Logout(ctx,SessionID) error` | revoke текущую family; clear cookie; немедленная блокировка API |
| `RevokeSession(ctx,UserID,SessionID)` | только собственный session, либо админский explicit account-disable audit |
| `AcceptInvite(ctx,Token,User) Membership` | verified email совпадает приглашению; lock invite; not expired; повтор idempotent; нельзя получить owner |

Refresh TTL=30 дней, access=300s. Web cookie `__Host-photo_refresh`, guest `__Host-photo_share`, Path=/, Secure, HttpOnly, SameSite=Lax, без Domain. Native refresh хранится только SecureStore; не записывать в AsyncStorage/log. Access хранится только в памяти. Токены в query запрещены.

CSRF: bootstrap endpoint выдаёт nonce token, привязанный к auth/guest session; mutating cookie requests требуют `X-CSRF-Token` constant-time match и exact Origin allowlist. Exchange OTP/guest без session требует exact Origin + JSON content-type + bootstrap double-submit nonce; local HTTP dev использует отдельные cookie names без __Host и явный dev profile. Native bearer API не требует CSRF, но refresh native принимает токен в JSON только при client_type native; same origin cookies не меняют этот режим.

Rate limit в PostgreSQL fixed windows: OTP email 5/hour, IP 20/hour, verify challenge max5, guest exchange IP 60/min и share 30/min; настройки versioned ENV. Ответы 429 с Retry-After; email enumeration не допускается. Mail payload AES-256-GCM с random nonce, key_id, expiry; SMTP worker забирает через function, после send удаляет ciphertext. Отправлять STARTTLS с проверкой hostname/cert либо implicit TLS, запрещать plaintext production. Message-ID фиксирован на challenge; неизвестный результат send может повторить письмо с тем же OTP.

## RBAC

Owner/Admin: все проекты tenant, settings/memberships; ownership transfer только Owner. Admin не может revoke/demote Owner. Editor: проекты по ACL, импорт, styles, runs, reviews, publish/share, metadata, explicit asset deletion с проверкой references. Viewer: read разрешённых проектов, downloads только policy. Tenant deletion Owner, sysadmin SMTP вне tenant API; system admin не получается из обычного owner membership.

Restricted project ACL не расширяет tenant role: Viewer с project editor остаётся viewer. Guest не является membership, не запускает AI, не видит styles/internal tasks/метаданные чужих uploads. Любой чужой UUID →404 после scope query; существование скрытого ресурса не раскрывается 403. 403 только когда principal вправе видеть ресурс, но не выполнять действие.

## ShareService

`CreateLink(tx,collection,policy)->{link,secret}`: CSPRNG32 bytes base64url, HMAC-SHA256 отдельным SHARE_TOKEN_PEPPER; plaintext только однократный ответ. Idempotency повтор создания не раскрывает secret второй раз: возвращает link_id и secret_unavailable; UI при потере создаёт новую ссылку. Это исключение из byte-identical replay явно указано в API.

`Exchange(share_id,secret,locale)->GuestSession` сравнивает HMAC; invalid/revoked/expired дают одинаковый LINK_UNAVAILABLE. `AuthorizeGuest(session,action,resource)` выполняется на каждом запросе и перед write/commit. Tenant/collection выводятся из DB link. Session TTL=min(12h,link expiry); policy_version mismatch требует нового exchange. Revoke links при archive/delete коллекции атомарно, signal cancel guest jobs, abort uploads; ранее подписанные URL живут в пределах TTL.

Tenant lifecycle реализован миграциями00097–00098 и `backend/internal/tenant`. Создание tenant требует live session и Idempotency-Key, атомарно создаёт tenant, owner membership, usage и audit. PATCH применяет admin ACL, системные границы quotas/retention и revision CAS. DELETE доступен owner, под tenant lock переводит tenant в deleting, отзывает share links/sessions, запрашивает отмену runs и создаёт replayable deletion job; resumable физическая очистка объектов и строк остаётся отдельной незавершённой maintenance-стадией.

Invitation create требует admin ACL и Idempotency-Key. CSPRNG token хранится только как SHA-256; recipient и JSON с token/locale/tenant зашифрованы AES-256-GCM и в одной SQL-транзакции связаны с invitation, mail outbox и audit. Mail worker распознаёт `kind=invite`, сохраняет стабильный Message-ID и очищает ciphertext через общий fenced finish. Accept под блокировкой token проверяет live session, exact normalized user email, TTL и active tenant; повтор тем же пользователем возвращает существующее membership, роль owner выдать нельзя. Live PostgreSQL/RabbitMQ delivery и concurrency пока не подтверждены.

Fragment `#k` прочитать client-side один раз, отправить POST body, очистить replaceState после успеха; не отправлять SSR, analytics, error capture. CSP self по scripts, allow storage domain images/connect; без third-party analytics. Cache-Control private,no-store для protected API/portal pages; noindex/nofollow и Referrer-Policy no-referrer. Ни robots, ни download=false не являются DRM.

## Worker и данные

Выбран task credentials endpoint: mTLS service identity + task_id/fence capability, срок <=lease_remaining; выдаёт read exact input keys и write только attempt prefix. MinIO STS support проверяется P01; если недоступен, использовать внутренний Go streaming object gateway с теми же ACL, не выдавать широкие static credentials. Native MPS подключается через loopback private TLS endpoint, не public Caddy. Этот gateway — определённый резервный транспорт, не архитектурная замена хранения.

У upload проверить signature/header до decode, bytes/пиксели/format, timeout, память, sandbox без внешней сети; TIFF multi-page/CMYK/PSD/SVG/HTML/video отвергнуть. Metadata extract без shell interpolation; filenames не аргументы shell. Archive и package import отвергают absolute paths, `..`, symlinks, zip bombs, oversized LUT arrays, duplicate JSON keys, NaN/Infinity. Никаких pickle/torch remote code из пользовательского файла. Веса только approved lock.

Audit: actor/principal, tenant, action, resource, revision/hash, time/request_id; не писать OTP, bearer/secret, signed URL query, raw prompts в общие логи. Prompt хранится в закрытом generation artifact. Metrics не включают user email/filename. Ключи шифрования и peppers ротируются с key_id и grace window; refresh/JWT kid allowlist, expired legacy keys удаляются после TTL. Backup шифруется отдельным ключом.
