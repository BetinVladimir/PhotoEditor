# V31 native signer deployment — implementation state

Это описание фактических точек сборки, не evidence принятого deployment. Tests NOT RUN.

1. Trusted process provisioning загружает квалифицированные C2PA SDK и PKCS#11 vendor library. SDK/native library identity проверяется независимым live authority по deployment locks; task JSON и env import hooks не выбирают код.
2. Создать `PKCS11RSAKey(pkcs11, library, key_handle=..., token_serial=..., key_id=..., certificate_chain=..., pin_supplier=..., check_active=...)`. Поддерживаемые алгоритмы данного provider: PS256/PS384/PS512. PIN supplier читает deployment secret; не передавать PIN через task, CLI arguments или logs. Провайдер не загружает private key bytes.
3. Передать `key.sign_payload` в `build_native_signer_executor` вместе с exact canonical policies, trust bundle, certificate chain, algorithm и independent native authority/installation inspector/embedded binding verifier. Inspector обязан проверять actual SDK/native lock, key availability/certificate algorithm/chain expiry/revocation/network controls. Verifier обязан доказать exact embedded binary store и policy certificate, а не только Trusted JSON.
4. Фабрика executor имеет сигнатуру `(database, worker_id, artifacts) -> Executor` и вызывает `build_native_signer_executor(database, worker_id, artifacts, policies, qualification_sha, native_authority, ...)`.
5. Вызвать `run_signer_worker(config, executor_factory)`. CPU-only config без imaging models/layers/delivery/interchange. Registration должна иметь signer execution_pool, только provenance.sign, exact SDK/certificate/trust/report pins и неистёкшую qualification. Transport использует targeted `q.worker.<worker-id>.cpu`; topology предварительно создаёт bootstrap.
6. Policies/qualification/cert/key rotation требуют новой process installation. Existing SQL/native checks выполняются до и после sensitive SDK/key calls; stale callback после закрытия signer отвергается.

## Оставшиеся обязательные работы

- Конкретные независимые SDK/native installation inspector и embedded manifest/certificate verifier.
- Native SDK lock и dependency qualification, release/runtime packaging, CLI provisioning без произвольного загрузчика модулей.
- Qualification certificates, trust bundle, token и secret source реального deployment.
- Квалификация RSA/EC/Ed25519 providers с реальными native SDK/HSM; код providers написан, но не проверен выполнением.
- Full lifecycle/signature tamper/adoption/revocation tests и измерение ресурсов на выбранном backend.

Существующие adapter/unit doubles не заменяют эти требования. V31 не завершён.

## Concrete RSA-PSS composition

Вместо ручной сборки шагов2–3 использовать `build_pkcs11_signer_executor`: передать deployment-loaded pkcs11/library, exact key_handle/token_serial/key_id, PIN supplier, PS256/PS384/PS512, trust/certificate bytes и обязательные independent native_inspector/native_authority/verify_binding. Фабрика проверяет key_handle всех canonical policies до key construction. Inspector проверяет current private-key possession fresh challenge и независимую native authority до/после; certificate/public-key binding доказан signature verification. Challenge не аттестует chain trust/revocation/native library provenance. Каждый inspector вызов делает live signing challenge: стоимость и HSM session/login behavior требуют qualification/измерения, кеширования health attestation пока нет.

## File-integrity layer (2026-10-11)

`PinnedRuntimeFiles(lock, sdk_lock_sha, files)` проверяет SHA-256 canonical lock формата `signer-runtime-files/v1`, exact inventory, размеры и байты deployment files без symlink/hardlink/group-world-write. Пример lock: `{"schema_version":"signer-runtime-files/v1","files":[{"name":"native","sha256":"<64 lowercase hex>","size_bytes":123}]}`. `files` — process-owned map logical name→absolute path. Не передавать пути из tasks. Максимум 256 файлов, 512 MiB каждый, 2 GiB всего; cancellation между блоками 1 MiB, сравнение inode/metadata до/после чтения и path identity.

`FileBoundInstallationInspector(files, live_inspector)` проверяет файлы до и после live authority, принимает только strict True. Deployment manifest, certificate/trust/PIN files, их директории и pinned runtime files должны принадлежать UID signer-процесса или root; файл другого UID отклоняется даже при корректном SHA и закрытых правах. `build_pkcs11_signer_executor(..., runtime_files=files)` включает этот слой и проверяет совпадение SDK lock всех policies до создания key provider. Для deployment с иным lock schema требуется отдельный approved binding, нельзя менять hash policy задним числом.

Хеши файлов НЕ доказывают, что native handle загрузил эти байты: независимая loaded-handle attestation, trust/revocation/network qualification остаются обязательными. Эта реализация не закрывает V31 и не подключает API автоматически. Unit-тесты написаны, НЕ запускались.


## ECDSA PKCS#11 (2026-10-11)

`PKCS11ECKey` реализует ES256/P-256, ES384/P-384 и ES512/P-521. До открытия token проверяется exact algorithm/curve. Данные callback хешируются соответствующим SHA2; token получает digest с явным CKM_ECDSA. Fixed-width r||s проверяется, преобразуется в DER для C2PA Python callback и криптографически сверяется с leaf certificate на каждом вызове. COSE-представление на выходе независимо проверяет существующий binary verifier. Session/PIN lifetime, exact token/key lookup, SIGN/SENSITIVE/non-EXTRACTABLE, expiry/cancellation и fresh health challenge повторяют требования RSA provider. Private key bytes не читаются.

`build_pkcs11_signer_executor` выбирает RSA/EC provider по exact allowlist алгоритмов. Ed25519 пока отклоняется. Unit-тесты реальной EC математики с fake token sessions и factory dispatch написаны, НЕ запускались. Нужны проверка поддерживаемых механизмов реального HSM, qualification SDK callback conversion и полный sign/adoption/revocation cycle.

Источники: https://python-pkcs11.readthedocs.io/en/latest/api.html ; https://github.com/contentauth/c2pa-python/blob/main/examples/sign.py .


## Ed25519 PKCS#11 (2026-10-11)

`PKCS11Ed25519Key` добавляет exact ED25519: leaf certificate обязан содержать Ed25519 public key, vendor API обязан иметь CKM_EDDSA/CKK_EC_EDWARDS. Применяется pure Ed25519 без prehash/context; token подписывает оригинальные callback bytes. Требуются ровно64 signature bytes и успешная криптографическая проверка по leaf certificate перед возвратом. Те же token serial/key ID, non-extractable sensitive signing key, certificate validity, cancellation, session cleanup и свежий challenge обязательны. Неподдерживаемый механизм не заменяется другим алгоритмом.

Process factory выбирает Ed25519 наряду с RSA/EC. Историческое предложение выше о запрете Ed25519 относится к состоянию до этого изменения. Tests NOT RUN: actual Ed25519 crypto с fake sessions, corrupt/short signature, unsafe attributes, wrong token/expiry/algorithm, certificate-bound fresh challenge и provider dispatch. Реальная поддержка vendor mechanism и квалификация всей цепочки остаются открытыми. API reference: https://python-pkcs11.readthedocs.io/en/latest/api.html .

## Process deployment manifest (2026-10-11)

`load_signer_deployment(absolute_root, manifest='installation.json')` загружает strict JSON `signer-deployment/v1`. Поля: `sdk_lock_sha`, `qualification_sha`, `policies` (inline SigningPolicy array1..256), `certificate_chain`/`trust_bundle` (`{path,sha256}`), `algorithm`, `key_handle`, `token_serial_hex`, `key_id_hex`, `pin_file`, `timestamp_services` (ID→HTTPS URL), `network_hosts`. Все поля обязательны, неизвестные запрещены. Paths строго относительные без traversal; directory traversal через dir_fd/O_NOFOLLOW, файлы regular, single-link, без group/world write. Manifest≤8MiB, certificate/trust≤1MiB каждый, policies≤8MiB total. Все policy SDK/cert/trust/key pins должны совпадать с установкой, policy IDs уникальны; timestamp/revocation требуют согласованного network allowlist.

Возвращается frozen `SignerDeployment` с immutable bytes/tuples. `deployment.build_executor(database, worker_id, artifacts, sdk=..., pkcs11=..., library=..., native_authority=..., native_inspector=..., verify_binding=...)` подключает существующий PKCS11 signer factory. Native modules/authorities передаются явно квалифицированным process code; manifest не выбирает Python imports или private key. PIN читается лениво при каждом key callback из file≤1024bytes, с запретом всех group/world permission bits; UTF-8, без trim. PIN не сохраняется в deployment object и не включается в task/CLI/logs. Secret mount должен иметь права0400/0600, доступные UID signer.

Это executable configuration loader, но ещё не standalone CLI/native loaded-handle authority или production provisioning. Unit-тесты config/pin/paths/permissions/factory написаны, НЕ запускались.

## Deployment lifecycle entrypoint (2026-10-11)

`run_deployed_signer(config, deployment_root, sdk=..., pkcs11=..., library=..., native_authority=..., native_inspector=..., verify_binding=..., runtime_files=...)` объединяет загрузчик deployment manifest с isolated signer factory, targeted transport и shared registration/heartbeat/shutdown lifecycle. Эта точка требует PinnedRuntimeFiles и совпадение lock SHA всех policies до создания transport; direct embedded factories остаются доступны для других квалифицированных installations. Запуск разрешён только на process main thread. Independent authorities и загруженные native handles обязательны и не выбираются через JSON/env import hooks.

Unit-тесты обязательной authority до загрузки и manifest→factory→lifecycle написаны, НЕ запускались. Это callable process entrypoint, а не готовый CLI/container: concrete loaded-handle inspector, packaging qualified binaries и operator provisioning остаются открытыми.

## Offline SDK wheel provisioning (2026-10-11)

`photo-worker signer-verify-sdk-wheel /absolute/path/package.whl` streams the wheel with the fixed c2pa-python0.32.4 Linux manylinux_2_28_x86_64 SHA from execution-contract15. It rejects symlinks/hardlinks, group/world writable files, nonregular/empty/over512MiB files, short reads, replacement and metadata changes; checks cancellation between bounded1MiB reads and closes its descriptor on all paths. Output is a distribution-byte receipt, never a qualification or loaded-library attestation. No extraction/import/install/network occurs.

`workers/requirements-signer-sdk.txt` pins the same version/hash and binary-only installation. On the isolated Linux x86_64 signer, use an offline wheelhouse and the qualified Python environment: `python -m pip install --no-index --find-links /absolute/approved-wheelhouse --no-deps -r workers/requirements-signer-sdk.txt`. pip repeats the hash check on the bytes it installs. Transitive dependencies, Python/native/container hashes and live authorities must be supplied by the complete qualified deployment lock. This command was NOT RUN; no package was installed. Synthetic unit-tests cover pinned hash refusal, bounded short reads/receipt, unsafe files, cancellation cleanup and same-bytes path replacement; NOT RUN. The official wheel itself was not read or qualified.


### V31 concrete certificate inspector — 2026-10-12

V31 concrete CertificateBoundInstallationInspector authored: exact policy certificate SHA, bounded PEM chain, all certificate validity windows before/after mandatory live authority, leaf RSA>=2048/PSS or exact EC curve/Ed25519 algorithm and digitalSignature KeyUsage. Rejects naive clock and policy mutation; never replaces native/trust/revocation/network authority. Added generated Ed25519 cert tests for pin/expiry/algorithm/usage/clock/live refusal/expiry during callback.

NOT RUN. Inspector not yet wired to deployed composition; real chain/HSM/native qualification and packaging remain required. No deployed signer completion claimed.


### V31 deployed certificate inspector composition — 2026-10-12

V31 SignerDeployment.build_executor now always wraps mandatory independent native_inspector with CertificateBoundInstallationInspector before PKCS11/native/file/key composition. Missing KeyUsage normalized to explicit refusal. Authored deployment factory type assertion, real generated certificate composition propagating live-authority refusal, and missing-authority refusal before factory/PIN work.

NOT RUN. Deployed composition source wired, but qualified native SDK/HSM/trust/revocation/network authorities, packaging and end-to-end execution remain open. No deployed signer completion credit.

### Offline deployment file preflight — 2026-10-12

`photo-worker signer-verify-deployment /absolute/deployment/root --runtime-file native=/absolute/native/library` reads `installation.json` and the relative `runtime-lock.json` through bounded, no-symlink deployment file reads. It requires exact policy SDK lock, certificate/trust pins and every named runtime file's size/SHA. It also checks the pinned certificate's current validity, leaf key algorithm and digitalSignature use through the same static predicate as the runtime inspector. A missing PIN file does not affect this check because no PIN is read. The JSON receipt contains only public hashes/count and `native_qualified:false`; it is never a signer activation receipt. Operator CLI tests are authored, NOT RUN. Real loaded-handle, chain trust, revocation, HSM and runtime qualification remain required.


### V31 certificate algorithm and cancellation boundaries — 2026-10-12

V31 deployed certificate inspector now enforces documented RSA2048–8192 bounds and refuses CA leaf certificates. Expanded generated-certificate coverage across PS256/384/512, P256/P384/P521 exact curves, Ed25519, weak/wrong/unknown algorithms, missing KeyUsage, CA leaf, callback policy mutation and cancellation. Independent live trust/native authority still mandatory; certificate checks do not replace chain trust/revocation.

NOT RUN. Synthetic certificates do not qualify production signer, SDK/HSM or deployment. Full lifecycle/packaging and remaining product requirements remain open.
