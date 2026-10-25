# NetStream Backend

Control plane NetStream: реестр сенсоров, версионированная desired-конфигурация агентов, реестр моделей. Управляет data plane (`agent` → Kafka → ClickHouse, см. `deploy`), но не является его частью — недоступность backend не останавливает захват/агрегацию/применение блок-листа на агенте, а падение агента не останавливает приём телеметрии остальными сенсорами.

Состояние control plane хранится в PostgreSQL, а не в ClickHouse: пользователи, конфиги, метаданные моделей и инциденты — это состояние, а не телеметрия построчно.

## Запуск

Нужен Go 1.24+, Postgres и S3-совместимое объектное хранилище (в `deploy` — сервис `objectstore`, SeaweedFS; см. его README).

```bash
docker run -d --name netstream-postgres -p 5432:5432 \
    -e POSTGRES_USER=netstream -e POSTGRES_PASSWORD=netstream-dev -e POSTGRES_DB=netstream_control_plane \
    postgres:16-alpine

go run ./cmd
```

Миграции (`internal/storage/postgres/migrations/*.sql`) применяются автоматически при старте, по одному разу каждая, версии — в таблице `schema_migrations` (тот же идемпотентный принцип, что у `deploy/clickhouse/migrate.sh`).

## Конфигурация

| Переменная | По умолчанию | Значение |
|---|---|---|
| `SERVER_PORT` | `9999` | Порт HTTP(S) API |
| `TLS_CERT_FILE` | нет | Путь к сертификату; вместе с `TLS_KEY_FILE` включает HTTPS вместо HTTP. Задан только один из двух — ошибка при старте |
| `TLS_KEY_FILE` | нет | Путь к приватному ключу |
| `TLS_CLIENT_CA_FILE` | нет | Путь к CA для проверки клиентских сертификатов — включает mTLS (требует `TLS_CERT_FILE`/`TLS_KEY_FILE`, иначе ошибка при старте). Без неё API принимает запросы без клиентского сертификата от кого угодно, кто достучится до порта — см. «mTLS» ниже |
| `POSTGRES_HOST` | `localhost` | Хост Postgres |
| `POSTGRES_PORT` | `5432` | Порт Postgres |
| `POSTGRES_USER` | `netstream` | Пользователь |
| `POSTGRES_PASSWORD` | `netstream-dev` | Пароль (значение по умолчанию — только для разработки) |
| `POSTGRES_DB` | `netstream_control_plane` | База |
| `POSTGRES_SSLMODE` | `disable` | `sslmode` в DSN |
| `OBJECTSTORE_ENDPOINT` | `localhost:8333` | Хост:порт S3-совместимого API |
| `OBJECTSTORE_ACCESS_KEY` | `netstream` | Access key |
| `OBJECTSTORE_SECRET_KEY` | `netstream-dev` | Secret key (значение по умолчанию — только для разработки) |
| `OBJECTSTORE_BUCKET` | `netstream-models` | Бакет для артефактов моделей; создаётся автоматически при старте, если его ещё нет |
| `OBJECTSTORE_USE_SSL` | `false` | TLS до объектного хранилища |
| `KAFKA_BOOTSTRAP_SERVERS` | нет | Включает коррелятор инцидентов (см. «Инциденты» ниже) — без неё Kafka не трогается вообще, как раньше |
| `KAFKA_DETECTIONS_TOPIC` | `netstream.detections.v1` | Топик, который читает коррелятор |
| `KAFKA_ALERTS_TOPIC` | `netstream.alerts.v1` | Топик, в который публикуется алерт на каждый новый (не продлённый) инцидент |
| `KAFKA_CONSUMER_GROUP` | `netstream-backend-incidents` | Своя consumer group — не конкурирует с `ml`'s онлайн-детектором или ClickHouse за тот же топик |
| `CA_CERT_FILE` | нет | Путь к сертификату CA, которым backend подписывает сертификаты сенсоров (см. «Выпуск сертификатов сенсоров» ниже) — без неё `/certificate`-маршруты отдают `503` |
| `CA_KEY_FILE` | нет | Приватный ключ этого CA, в формате PKCS#8 (`openssl pkcs8 -topk8 -nocrypt`, если исходно в другом формате) |
| `CERT_VALIDITY_DAYS` | `90` | Срок действия выпускаемого сертификата сенсора |

## API

| Метод | Путь | Значение |
|---|---|---|
| `POST` | `/api/v1/sensors/register` | Регистрация/heartbeat сенсора по `host_id`; `hostname`/`agent_version` обновляются, `config_version` — нет (двигается только через `SetDesiredConfig`) |
| `GET` | `/api/v1/sensors` | Список сенсоров |
| `GET` | `/api/v1/sensors/:host_id` | Один сенсор |
| `GET` | `/api/v1/sensors/:host_id/config` | Текущий desired-конфиг (последняя версия) |
| `PUT` | `/api/v1/sensors/:host_id/config` | Новая версия конфига — тело запроса целиком становится новым `payload`, `config_version` увеличивается на 1 в той же транзакции |
| `PUT` | `/api/v1/sensors/:host_id/config/ack` | Агент подтверждает версию, которую реально применил, и список полей этой версии, которые применить не удалось (`{"version": N, "unapplied_fields": [...]}`) — записывается в `actual_config_version`/`actual_config_unapplied_fields`, отдельно от desired `config_version` |
| `POST` | `/api/v1/models` | Регистрация версии модели (идемпотентно по `name`+`version`) |
| `GET` | `/api/v1/models` | Список моделей |
| `GET` | `/api/v1/models/:name/:version` | Одна модель |
| `PUT` | `/api/v1/models/:name/:version/artifact` | Загрузить артефакт (тело запроса целиком, `Content-Length` обязателен); модель должна быть уже зарегистрирована. Заголовки `X-Artifact-Sha256`/`X-Artifact-Signature` обязательны (см. ниже) |
| `GET` | `/api/v1/models/:name/:version/artifact` | `302` на временную (15 минут) прямую ссылку на артефакт в объектном хранилище |
| `PUT` | `/api/v1/models/:name/:version/onnx` | То же самое для ONNX-экспорта той же модели (`--export-onnx`) — отдельный, необязательный артефакт на той же записи каталога, те же заголовки целостности |
| `GET` | `/api/v1/models/:name/:version/onnx` | `302` на временную ссылку на ONNX-артефакт |
| `GET` | `/api/v1/incidents` | Список инцидентов, самые недавние первыми |
| `GET` | `/api/v1/incidents/:id` | Один инцидент |
| `PUT` | `/api/v1/incidents/:id/close` | Закрыть открытый инцидент; повторное закрытие — `404` |
| `POST` | `/api/v1/sensors/:host_id/certificate` | Подписать CSR (тело запроса — PEM) как сертификат сенсора `:host_id`; см. «Выпуск сертификатов сенсоров» ниже |
| `GET` | `/api/v1/sensors/:host_id/certificate` | История выпущенных сертификатов сенсора |
| `PUT` | `/api/v1/sensors/:host_id/certificate/revoke` | Отметить самый свежий активный сертификат отозванным; если активного нет — `404` |

Конфигурация применяется как целый снапшот, а не набор изменений по одному: `SetDesiredConfig` в одной транзакции вставляет новую строку в `agent_configs` и увеличивает `sensors.config_version`, поэтому наблюдатель никогда не увидит версию, для которой ещё нет соответствующего конфига.

`config_version` (desired) и `actual_config_version` (что агент реально подтвердил) — разные поля: агент может быть недоступен, применять конфиг медленно или не суметь применить часть полей (см. `agent/README.md`, раздел «Control plane» — `response_mode`/`quarantine_ttl_secs` пока не хот-эплаятся вовсе), и `sensors.list`/`sensors.get` показывают оба значения, а не молча предполагают, что агент уже сходится с последней версией.

`actual_config_version = N` **не означает**, что версия N применена полностью — агент подтверждает всю версию целиком даже если часть её полей он хот-эплаить не умеет (см. выше), и раньше это было неотличимо от настоящей конвергенции. `actual_config_unapplied_fields` — список ключей payload'а этой версии, которые агент не смог применить (пусто — применено полностью); обновляется вместе с `actual_config_version` и только когда подтверждаемая версия становится новым максимумом (запоздавший ack со старой версией не перезатирает список более свежего).

`AckActualConfig` также проверяет, что подтверждаемая версия реально существует: не больше текущего desired `config_version` сенсора и есть соответствующая строка в `agent_configs` — раньше `GREATEST` принимал любое число, включая версию чужого сенсора или версию, которой никогда не было. Обе проверки возвращают `400 invalid config version`, а не создают противоречивое состояние в БД. `GREATEST` (а не прямое присваивание) остаётся для самого номера версии — повторная (запоздавшая) доставка ack со старой версией не может откатить `actual_config_version` назад.

Реестр моделей хранит только метаданные (алгоритм, версию признаков, ссылку на датасет, git-ревизию агента, метрики, `artifact_uri`, `artifact_sha256`, `artifact_signature`, и то же самое для ONNX — `onnx_artifact_uri`, `onnx_artifact_sha256`, `onnx_artifact_signature`) — сами байты артефакта лежат в объектном хранилище (`internal/storage/objectstore`, S3 API — работает с любым S3-совместимым сервером, не только с конкретным вендором), в базе — только путь до него (`objectstore://<bucket>/<key>`). Скачивание отдаёт не сами байты, а `302`-редирект на подписанную (presigned) ссылку — backend не проксирует потенциально большой файл через себя.

```bash
curl -X PUT localhost:9999/api/v1/models/isolation-forest/v1/artifact \
    --data-binary @model.bin -H "Content-Type: application/octet-stream" \
    -H "X-Artifact-Sha256: <sha256-от-model.bin>" -H "X-Artifact-Signature: <Ed25519-подпись-над-sha256>"
curl -L localhost:9999/api/v1/models/isolation-forest/v1/artifact -o model.bin
```

ONNX-артефакт — та же пара запросов на `.../onnx` вместо `.../artifact`, с теми же заголовками над sha256 ONNX-файла. Это отдельная запись от `artifact_uri`: модель может быть зарегистрирована с `joblib`-артефактом, с ONNX-артефактом, с обоими или ни с одним — `--register-model --export-onnx` в `ml` выставляет оба за один прогон, но ни один не предполагает другой.

**Целостность артефакта.** `joblib.load()` (то, чем `ml` разворачивает скачанный `joblib`-артефакт) выполняет произвольный код при десериализации произвольных байт — ни backend, ни объектное хранилище не являются доверенной границей для этого сами по себе. `X-Artifact-Sha256`/`X-Artifact-Signature` обязательны при загрузке именно поэтому: backend **не проверяет** их (у него нет и не будет приватного ключа — проверка происходит только на стороне `ml`, см. `ml/docs/online-detector.md`, раздел «Сохранение и регистрация модели (M14)»), он лишь сохраняет то, что заявил публикующий, как часть каталожной записи. Отсутствие заголовков — `400`, а не молчаливая публикация без интеграции. ONNX не выполняет произвольный код при загрузке так же, как `joblib.load()`, но несёт те же заголовки по тому же правилу — один путь загрузки для обоих типов артефакта, а не два разных.

## TLS

Серверный TLS (`TLS_CERT_FILE`/`TLS_KEY_FILE`) закрывает подмену desired-конфига на пути между backend и агентом (см. `agent/README.md`, раздел «Control plane», и `docs/adr/ADR-005-response-model.md`), но сам по себе не аутентифицирует вызывающего — без клиентского сертификата любой, кто достучится до порта, может читать/менять конфигурацию сенсоров и публиковать модели.

Самоподписанный сертификат сервера для лаборатории:

```bash
openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
    -keyout tls.key -out tls.crt -subj "/CN=localhost" \
    -addext "basicConstraints=critical,CA:FALSE" \
    -addext "keyUsage=digitalSignature,keyEncipherment" \
    -addext "extendedKeyUsage=serverAuth" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"
TLS_CERT_FILE=tls.crt TLS_KEY_FILE=tls.key go run ./cmd/main.go
```

`-addext` не опциональны: `openssl req -x509` по умолчанию помечает сертификат как `CA:TRUE`, что rustls (используемый агентом) отвергает как невалидный serving-сертификат (`CaUsedAsEndEntity`) — проверено вживую при подготовке этой фичи, просто `-subj` без остальных `-addext` даёт сертификат, который backend принимает, а агент — нет.

Агент должен указать этот же сертификат в `CONTROL_PLANE_CA_FILE` (см. `agent/README.md`), иначе не примет его как доверенный — самоподписанный сертификат не входит ни в один системный список корневых центров сертификации.

## mTLS (аутентификация и авторизация)

`TLS_CLIENT_CA_FILE` требует клиентский сертификат на каждый запрос (`tls.RequireAndVerifyClientCert`) и определяет личность и роль вызывающего по его сертификату — без отдельной системы пользователей/токенов:

- **`CommonName`** — идентификатор вызывающего: `host_id` сенсора или имя оператора.
- **`OrganizationalUnit`** определяет роль: `operator` — полный доступ (чтение и запись); `operator-readonly` — то же чтение, но без права менять что-либо (аудитор, дашборд); любой другой (или пустой) `OU` — сенсор, ограниченный собственным `host_id`.

Правила авторизации (`internal/api/auth.go`):

| Маршрут | Сенсор | Оператор (readonly) | Оператор (полный) |
|---|---|---|---|
| `POST /sensors/register` | только свой `host_id` в теле запроса | 403 | любой |
| `GET /sensors` | 403 | да | да |
| `GET /sensors/:host_id`, `.../config` | только свой `:host_id` | любой | любой |
| `PUT /sensors/:host_id/config/ack` | только свой `:host_id` | 403 (кроме своего, но у оператора своего `host_id` не бывает) | любой |
| `PUT /sensors/:host_id/config` | 403 (desired-конфиг назначает оператор, не сам сенсор) | 403 | да |
| `GET /models`, `GET /models/:name/:version`, `GET .../artifact`, `GET .../onnx` | 403 | да | да |
| `POST /models`, `PUT /models/:name/:version/artifact`, `PUT .../onnx` | 403 | 403 | да |
| `GET /incidents`, `GET /incidents/:id` | 403 | да | да |
| `PUT /incidents/:id/close` | 403 | 403 | да |

`operator-readonly` закрывает конкретный, названный во внешнем ревью, случай: наблюдатель/дашборд/аудитор, которому нужно видеть сенсоры, модели и инциденты, но не должно быть возможности что-либо менять — раньше единственной альтернативой было давать полный `operator`-сертификат или вообще обходиться без мониторинга. Более гранулярные права внутри самой роли оператора (например, «может закрывать инциденты, но не публиковать модели») за пределами read/write — не описаны, за пределами объёма этой правки.

Без `TLS_CLIENT_CA_FILE` поведение не меняется (как раньше — без аутентификации вообще; при старте без неё backend пишет в лог явное предупреждение, что порт не должен быть доступен за пределами доверенной сети).

Сертификаты для лаборатории — тот же CA подписывает серверный и клиентские:

```bash
# CA
openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
    -keyout ca.key -out ca.crt -subj "/CN=netstream-test-ca" \
    -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
    -addext "keyUsage=critical,keyCertSign,cRLSign"

# клиентский сертификат сенсора — OU не указан, значит роль "сенсор"
openssl req -newkey rsa:2048 -nodes -keyout sensor-1.key -out sensor-1.csr -subj "/CN=sensor-1"
openssl x509 -req -in sensor-1.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out sensor-1.crt -days 365 \
    -extfile <(printf "basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=clientAuth")

# клиентский сертификат оператора — OU=operator даёт полный доступ
openssl req -newkey rsa:2048 -nodes -keyout operator.key -out operator.csr -subj "/CN=alice/OU=operator"
openssl x509 -req -in operator.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out operator.crt -days 365 \
    -extfile <(printf "basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=clientAuth")

# клиентский сертификат оператора только на чтение — OU=operator-readonly
openssl req -newkey rsa:2048 -nodes -keyout readonly.key -out readonly.csr -subj "/CN=bob/OU=operator-readonly"
openssl x509 -req -in readonly.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out readonly.crt -days 365 \
    -extfile <(printf "basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=clientAuth")

TLS_CERT_FILE=server.crt TLS_KEY_FILE=server.key TLS_CLIENT_CA_FILE=ca.crt go run ./cmd
```

Проверено вживую (настоящий handshake): `bob` (`OU=operator-readonly`) получает `200` на `GET /sensors`/`GET /incidents`, но `403` на `POST /models` и `PUT /sensors/:host_id/config`; `alice` (`OU=operator`) проходит оба.

Проверено вживую (настоящий handshake, не только unit-тесты `internal/api/auth_test.go`): запрос без клиентского сертификата — TLS-алерт `certificate required`, ещё до того как запрос доходит до какого-либо обработчика; `sensor-1` может зарегистрировать и прочитать только `sensor-1`, получает 403 на `GET /sensors` и на конфиг `sensor-2`; `alice` (`OU=operator`) проходит везде.

Выпуск и ротация клиентских сертификатов сенсорам (сама PKI — удостоверяющий центр, выдача сертификата при первом подключении агента, отзыв) — отдельная задача, не описанная здесь: этот раздел — про то, как backend проверяет уже выпущенный сертификат, а не про то, как его получить агенту.

## Инциденты

`netstream.detections.v1` (`ml`'s онлайн-детектор, см. `ml/docs/online-detector.md`) несёт одну строку на каждое оценённое окно, атака или нет — слишком много для оператора, чтобы смотреть на это напрямую. `internal/incidents` + `internal/detections` сворачивают поток детекций в инциденты: включается через `KAFKA_BOOTSTRAP_SERVERS` (см. «Конфигурация» выше), читает `netstream.detections.v1` своей consumer group (не конкурирует за него ни с `ml`, ни с ClickHouse) и для каждой детекции с `is_attack: true`:

- если для той же пары `(host_id, src_ip)` уже есть открытый инцидент, чей `last_seen` не старше 5 минут — продлевает его (`last_seen`, `detection_count += 1`, `max_score = GREATEST(...)`, `severity` пересчитывается по `detection_count`: `low` < 5, `medium` < 20, `high` ≥ 20);
- иначе открывает новый инцидент и публикует в `netstream.alerts.v1` короткое сообщение (`incident_id`, `host_id`, `src_ip`, `severity`, `opened_at`) — один раз на инцидент, а не на каждую детекцию, чтобы потребителю алертов не нужно было дедуплицировать самому.

На пару `(host_id, src_ip)` не может быть больше одного открытого инцидента (частичный уникальный индекс `incidents_open_host_src_ip` в миграции `0005`) — закрытый инцидент не блокирует открытие нового для той же пары позже, это уже другое событие, не продолжение старого.

Это не Correlation Engine (кластеризация между хостами, подавление дублей по правилам, SLO) — он осознанно вне объёма диплома (см. `docs/architecture/system.md`, «Дальше»). Это минимум, без которого «детект → блок» заканчивается на сыром потоке детекций, а не на том, что оператор может увидеть списком и закрыть.

Проверено вживую (реальный Postgres + реальный одноброкерный Kafka из `deploy/compose.yml`, без моков): публикация настоящей детекции в `netstream.detections.v1` реально создаёт инцидент (виден в `GET /api/v1/incidents`) и реально публикует алерт в `netstream.alerts.v1`; вторая детекция для той же пары в течение 5 минут продлевает тот же инцидент (`detection_count` растёт, `max_score` обновляется), а не открывает новый и не публикует второй алерт; `PUT /api/v1/incidents/:id/close` реально закрывает инцидент, повторное закрытие — `404`.

## Выпуск сертификатов сенсоров

Раньше backend умел только проверять уже выпущенный клиентский сертификат (раздел «mTLS» выше), а выпуск был внешней, не описанной здесь задачей. `internal/pki` закрывает минимум: backend сам подписывает CSR сенсора своим CA (`CA_CERT_FILE`/`CA_KEY_FILE`), оператор (`requireWriteOperator`, см. «mTLS» выше) инициирует выпуск от имени сенсора — самообслуживания (sensor enrolls itself) нет, потому что у только что развёрнутого сенсора ещё нет сертификата, которым можно было бы аутентифицировать такой запрос.

```bash
# на сенсоре: сгенерировать ключ и CSR, приватный ключ никуда не передаётся
openssl ecparam -name prime256v1 -genkey -noout -out sensor-1.key
openssl req -new -key sensor-1.key -out sensor-1.csr -subj "/CN=sensor-1"

# оператор подписывает CSR от имени сенсора
curl --cert operator.crt --key operator.key -X POST \
    https://localhost:9999/api/v1/sensors/sensor-1/certificate \
    --data-binary @sensor-1.csr -o sensor-1.crt
```

`Issue` (`internal/pki/pki.go`) проверяет CSR до того, как подписать его: `CommonName` обязан совпадать с `:host_id` из пути (сенсор не может запросить сертификат на чужое имя), а `OrganizationalUnit` не может содержать `operator`/`operator-readonly` — иначе выпуск сертификата сенсора стал бы способом самому выдать себе роль оператора через поле, которое backend не перепроверяет больше нигде. `CA_KEY_FILE` — приватный ключ CA в формате PKCS#8 (`openssl pkcs8 -topk8 -nocrypt -in ca.key -out ca.key.pkcs8`, если ключ сгенерирован в другом формате); `x509.ParsePKCS8PrivateKey` принимает RSA/ECDSA/Ed25519 одинаково, без привязки к конкретному алгоритму CA.

Каждый выпуск записывается в таблицу `sensor_certificates` (serial, срок действия, когда выпущен); `PUT .../certificate/revoke` помечает самый свежий активный сертификат отозванным. **Это не отзыв в смысле PKI** — у Go `tls.Config` нет точки интеграции с CRL/OCSP при `RequireAndVerifyClientCert`, поэтому отозванный сертификат продолжает проходить TLS-хендшейк до истечения срока действия; `revoked_at` — это запись для оператора («этот сертификат больше не должен считаться легитимным, разберись руками» — например, обновить desired-конфиг сенсора на карантин), а не контроль, который backend сам применяет на каждый запрос. Настоящая проверка отзыва на каждом handshake — отдельная задача, не нужная для объёма диплома.

Проверено вживую (реальный CA на эллиптической кривой, реальный CSR, реальный mTLS-хендшейк): оператор подписывает CSR сенсора, сенсор реально регистрируется своим новым сертификатом и собственным приватным ключом через `POST /sensors/register`; CSR с `OU=operator` реально отклоняется (`400`) до подписания — самоэскалация роли через выпуск сертификата невозможна; повторный отзыв уже отозванного сертификата — реальный `404`, а не тихий no-op.

## Пока не реализовано

Более гранулярные права внутри самой роли оператора, чем просто чтение/запись (см. `operator-readonly` выше — это закрывает базовый случай «наблюдатель», не произвольную матрицу прав); реальное применение отзыва сертификата на TLS-хендшейке (CRL/OCSP) — выше `revoked_at` остаётся лишь записью для оператора, не контролем.
