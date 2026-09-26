# NetStream Backend

Control plane NetStream: реестр сенсоров, версионированная desired-конфигурация агентов, реестр моделей. Управляет data plane (`agent` → Kafka → ClickHouse, см. `deploy`), но не является его частью — недоступность backend не останавливает захват/агрегацию/применение блок-листа на агенте, а падение агента не останавливает приём телеметрии остальными сенсорами.

Состояние control plane хранится в PostgreSQL, а не в ClickHouse: пользователи, конфиги, метаданные моделей и инциденты — это состояние, а не телеметрия построчно.

## Запуск

Нужен Go 1.24+ и Postgres.

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
| `SERVER_PORT` | `9999` | Порт HTTP API |
| `POSTGRES_HOST` | `localhost` | Хост Postgres |
| `POSTGRES_PORT` | `5432` | Порт Postgres |
| `POSTGRES_USER` | `netstream` | Пользователь |
| `POSTGRES_PASSWORD` | `netstream-dev` | Пароль (значение по умолчанию — только для разработки) |
| `POSTGRES_DB` | `netstream_control_plane` | База |
| `POSTGRES_SSLMODE` | `disable` | `sslmode` в DSN |

## API

| Метод | Путь | Значение |
|---|---|---|
| `POST` | `/api/v1/sensors/register` | Регистрация/heartbeat сенсора по `host_id`; `hostname`/`agent_version` обновляются, `config_version` — нет (двигается только через `SetDesiredConfig`) |
| `GET` | `/api/v1/sensors` | Список сенсоров |
| `GET` | `/api/v1/sensors/:host_id` | Один сенсор |
| `GET` | `/api/v1/sensors/:host_id/config` | Текущий desired-конфиг (последняя версия) |
| `PUT` | `/api/v1/sensors/:host_id/config` | Новая версия конфига — тело запроса целиком становится новым `payload`, `config_version` увеличивается на 1 в той же транзакции |
| `POST` | `/api/v1/models` | Регистрация версии модели (идемпотентно по `name`+`version`) |
| `GET` | `/api/v1/models` | Список моделей |
| `GET` | `/api/v1/models/:name/:version` | Одна модель |

Конфигурация применяется как целый снапшот, а не набор изменений по одному: `SetDesiredConfig` в одной транзакции вставляет новую строку в `agent_configs` и увеличивает `sensors.config_version`, поэтому наблюдатель никогда не увидит версию, для которой ещё нет соответствующего конфига.

Реестр моделей хранит только метаданные (алгоритм, версию признаков, ссылку на датасет, git-ревизию агента, метрики, `artifact_uri`) — сам артефакт лежит отдельно (диск/объектное хранилище), в базе — только путь к нему.

## Пока не реализовано

RBAC, инциденты (таблица `incidents` создана, обработчиков ещё нет — сначала нужен M16, генерирующий детекции, которые в инциденты превращать), фактическая доставка desired-конфига агенту (сейчас backend хранит и отдаёт конфиг по запросу, но агент его ещё не запрашивает и не применяет — это следующий шаг на стороне `agent`), артефакты моделей в объектном хранилище (сейчас `artifact_uri` — произвольная строка, MinIO не подключён).
