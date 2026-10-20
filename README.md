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

## API

| Метод | Путь | Значение |
|---|---|---|
| `POST` | `/api/v1/sensors/register` | Регистрация/heartbeat сенсора по `host_id`; `hostname`/`agent_version` обновляются, `config_version` — нет (двигается только через `SetDesiredConfig`) |
| `GET` | `/api/v1/sensors` | Список сенсоров |
| `GET` | `/api/v1/sensors/:host_id` | Один сенсор |
| `GET` | `/api/v1/sensors/:host_id/config` | Текущий desired-конфиг (последняя версия) |
| `PUT` | `/api/v1/sensors/:host_id/config` | Новая версия конфига — тело запроса целиком становится новым `payload`, `config_version` увеличивается на 1 в той же транзакции |
| `PUT` | `/api/v1/sensors/:host_id/config/ack` | Агент подтверждает версию, которую реально применил (`{"version": N}`) — записывается в `actual_config_version`, отдельно от desired `config_version` |
| `POST` | `/api/v1/models` | Регистрация версии модели (идемпотентно по `name`+`version`) |
| `GET` | `/api/v1/models` | Список моделей |
| `GET` | `/api/v1/models/:name/:version` | Одна модель |
| `PUT` | `/api/v1/models/:name/:version/artifact` | Загрузить артефакт (тело запроса целиком, `Content-Length` обязателен); модель должна быть уже зарегистрирована. Заголовки `X-Artifact-Sha256`/`X-Artifact-Signature` обязательны (см. ниже) |
| `GET` | `/api/v1/models/:name/:version/artifact` | `302` на временную (15 минут) прямую ссылку на артефакт в объектном хранилище |

Конфигурация применяется как целый снапшот, а не набор изменений по одному: `SetDesiredConfig` в одной транзакции вставляет новую строку в `agent_configs` и увеличивает `sensors.config_version`, поэтому наблюдатель никогда не увидит версию, для которой ещё нет соответствующего конфига.

`config_version` (desired) и `actual_config_version` (что агент реально подтвердил) — разные поля: агент может быть недоступен, применять конфиг медленно или не суметь применить часть полей (см. `agent/README.md`, раздел «Control plane» — `response_mode`/`quarantine_ttl_secs` пока не хот-эплаятся вовсе), и `sensors.list`/`sensors.get` показывают оба значения, а не молча предполагают, что агент уже сходится с последней версией. `AckActualConfig` использует `GREATEST`, а не прямое присваивание — повторная (запоздавшая) доставка ack со старой версией не может откатить `actual_config_version` назад.

Реестр моделей хранит только метаданные (алгоритм, версию признаков, ссылку на датасет, git-ревизию агента, метрики, `artifact_uri`, `artifact_sha256`, `artifact_signature`) — сами байты артефакта лежат в объектном хранилище (`internal/storage/objectstore`, S3 API — работает с любым S3-совместимым сервером, не только с конкретным вендором), в базе — только путь до него (`objectstore://<bucket>/<key>`). Скачивание отдаёт не сами байты, а `302`-редирект на подписанную (presigned) ссылку — backend не проксирует потенциально большой файл через себя.

```bash
curl -X PUT localhost:9999/api/v1/models/isolation-forest/v1/artifact \
    --data-binary @model.bin -H "Content-Type: application/octet-stream" \
    -H "X-Artifact-Sha256: <sha256-от-model.bin>" -H "X-Artifact-Signature: <Ed25519-подпись-над-sha256>"
curl -L localhost:9999/api/v1/models/isolation-forest/v1/artifact -o model.bin
```

**Целостность артефакта.** `joblib.load()` (то, чем `ml` разворачивает скачанный артефакт) выполняет произвольный код при десериализации произвольных байт — ни backend, ни объектное хранилище не являются доверенной границей для этого сами по себе. `X-Artifact-Sha256`/`X-Artifact-Signature` обязательны при загрузке именно поэтому: backend **не проверяет** их (у него нет и не будет приватного ключа — проверка происходит только на стороне `ml`, см. его README, раздел «Сохранение и регистрация модели (M14)»), он лишь сохраняет то, что заявил публикующий, как часть каталожной записи. Отсутствие заголовков — `400`, а не молчаливая публикация без интеграции.

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
- **`OrganizationalUnit` содержит `operator`** — вызывающий оператор (полный доступ); любой другой (или пустой) `OU` — сенсор, ограниченный собственным `host_id`.

Правила авторизации (`internal/api/auth.go`):

| Маршрут | Сенсор | Оператор |
|---|---|---|
| `POST /sensors/register` | только свой `host_id` в теле запроса | любой |
| `GET /sensors` | 403 | да |
| `GET /sensors/:host_id`, `.../config`, `PUT .../config/ack` | только свой `:host_id` | любой |
| `PUT /sensors/:host_id/config` | 403 (desired-конфиг назначает оператор, не сам сенсор) | да |
| `/models/*` (регистрация, список, загрузка/скачивание артефакта) | 403 | да |

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

TLS_CERT_FILE=server.crt TLS_KEY_FILE=server.key TLS_CLIENT_CA_FILE=ca.crt go run ./cmd
```

Проверено вживую (настоящий handshake, не только unit-тесты `internal/api/auth_test.go`): запрос без клиентского сертификата — TLS-алерт `certificate required`, ещё до того как запрос доходит до какого-либо обработчика; `sensor-1` может зарегистрировать и прочитать только `sensor-1`, получает 403 на `GET /sensors` и на конфиг `sensor-2`; `alice` (`OU=operator`) проходит везде.

Выпуск и ротация клиентских сертификатов сенсорам (сама PKI — удостоверяющий центр, выдача сертификата при первом подключении агента, отзыв) — отдельная задача, не описанная здесь: этот раздел — про то, как backend проверяет уже выпущенный сертификат, а не про то, как его получить агенту.

## Пока не реализовано

RBAC сверх ролей "сенсор"/"оператор" из mTLS (более гранулярные права внутри роли оператора), инциденты (таблица `incidents` создана, обработчиков ещё нет — сначала нужен M16, генерирующий детекции, которые в инциденты превращать), выпуск и ротация клиентских сертификатов сенсорам (сама PKI, не её проверка).
