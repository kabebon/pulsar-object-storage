# Архитектура Пульсар

Пульсар — production-ready SaaS для объектного хранилища с доставкой через CDN.
Этот документ описывает архитектуру, потоки данных и ключевые технические решения,
чтобы новый инженер мог за час начать работать с кодовой базой.

## Обзор системы

```
                    ┌──────────────────────────────────────────┐
                    │              Клиенты                      │
                    │  браузер (htmx) · curl · AWS SDK · rclone │
                    └───────────────┬──────────────────────────┘
                                    │ HTTPS
                                    ▼
                         ┌──────────────────────┐
                         │   Caddy (TLS, CDN)   │   ← on-demand TLS для кастомных доменов
                         └──────────┬───────────┘
                                    │
                ┌───────────────────┴───────────────────┐
                ▼                                        ▼
        ┌──────────────┐   ┌──────────────────────────────────────┐
        │  Pulsar API  │   │  Pulsar Web (htmx + templ + CSRF)    │
        │  /api/v1/*   │   │  /app/*  ·  /  ·  /pricing           │
        │ Bearer auth  │   │  session cookie auth                 │
        └──────┬───────┘   └─────────────────┬────────────────────┘
               │                              │
               └──────────┬───────────────────┘
                          ▼
            ┌─────────────────────────┐
            │   service layer         │   ← бизнес-логика, квоты
            │  auth · storage · keys  │
            │  billing · domain       │
            └─────┬───────────────┬───┘
                  ▼               ▼
        ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
        │ PostgreSQL   │   │    Redis     │   │  S3 (MinIO)  │
        │ users,       │   │ sessions,    │   │  objects     │
        │ buckets,     │   │ rate-limit,  │   │  presigned   │
        │ billing...   │   │  cache       │   │  URLs        │
        └──────────────┘   └──────────────┘   └──────┬───────┘
                                                   │ public URL
                                                   ▼
                                          ┌─────────────────┐
                                          │  CDN edge       │
                                          │  (Cloudflare/   │
                                          │   CloudFront)   │
                                          └─────────────────┘
```

## Слои приложения

Кодовая база следует классической многослойной архитектуре (Clean-ish). Каждый
слой зависит только от нижележащего; HTTP-концерны не утекают в бизнес-логику.

| Слой | Пакет | Ответственность |
|------|-------|-----------------|
| **Transport** | `internal/handler/web`, `internal/handler/api` | Разбор HTTP-запросов, рендеринг templ, JSON, RFC 9457 ошибки |
| **Middleware** | `internal/middleware` | request-id, auth, csrf, rate-limit, metrics, recover |
| **Service** | `internal/service` | бизнес-логика: auth, storage, api-keys, password |
| **Domain services** | `internal/billing`, `internal/domain` | Stripe-интеграция, DNS-проверка |
| **Repository** | `internal/repository` | SQL-запросы через pgx, без ORM |
| **Infrastructure** | `internal/storage/s3`, `internal/cache`, `internal/mailer` | адаптеры к внешним системам |
| **Models** | `internal/models` | доменные типы и sentinel-ошибки |
| **Composition root** | `cmd/pulsar/main.go` | внедрение зависимостей, запуск |

## Потоки данных

### Регистрация и вход
1. `POST /register` → `AuthService.Register` хеширует пароль (bcrypt cost 12),
   создаёт пользователя в статусе `unverified`, генерирует одноразовый токен.
2. `Mailer.Send` отправляет ссылку через SMTP (Mailpit локально).
3. `GET /verify-email?token=...` → `AuthService.VerifyEmail` помечает email
   подтверждённым, переводит статус в `active`.
4. Триггер БД `assign_default_plan` выдаёт бесплатную подписку.
5. `POST /login` → проверка пароля, создание сессии в Redis, выдача cookie.

### Загрузка файла (presigned upload)
1. Клиент (браузер или AWS SDK) запрашивает `POST /buckets/{id}/objects/presign-upload`.
2. `StorageService.PresignUpload` проверяет квоту хранилища, строит S3-ключ
   вида `{user_id}/{bucket_name}/{object_key}` и подписывает PUT URL (TTL из конфига).
3. Клиент грузит файл **напрямую** в MinIO/S3 — данные не проходят через приложение.
4. `POST /objects/confirm` фиксирует метаданные объекта (размер, etag, sha256).

### Кастомный домен + on-demand TLS
1. Пользователь добавляет домен → получает CNAME и TXT-запись.
2. `POST /domains/{id}/verify` выполняет реальный `net.LookupTXT`/`LookupCNAME`.
3. Caddy при входящем HTTPS-запросе на новый домен спрашивает
   `GET /api/v1/domains/verify-tls?domain=...` — если 200, выпускает сертификат.

## Безопасность

- **Пароли**: bcrypt cost 12, ограничение 8-72 символов.
- **Сессии**: 256-битные случайные id, хранятся в Redis (не в JWT — позволяет
  мгновенный logout), sliding expiration, HttpOnly + SameSite=Strict.
- **CSRF**: gorilla/csrf на веб-формах; API использует Bearer-токены (иммунен).
- **API-ключи**: формат `pk_live_<32 bytes>`, в БД хранится только SHA-256.
- **Rate-limiting**: Redis-backed sliding window, per-IP и per-email на auth.
- **Audit log**: каждое значимое действие (login, key creation, bucket delete).
- **SQL**: только параметризованные запросы через pgx — никакой конкатенации.
- **Errors**: единый контракт RFC 9457 problem+json, без утечки внутренних деталей.

## Масштабирование

- **Stateless app**: сессии и rate-limit в Redis → горизонтальное масштабирование.
- **Connection pooling**: pgxpool с настраиваемым `MaxConns`/`MinConns`.
- **Presigned uploads**: трафик не проходит через приложение — S3 принимает напрямую.
- **CDN**: публичные объекты кэшируются на edge-узлах; origin нагружен только miss'ами.

## Наблюдаемость

- `/metrics` — Prometheus (счётчики запросов, гистограммы latency, in-flight gauge).
- `/healthz` — liveness (всегда 200 если процесс жив).
- `/readyz` — readiness (проверяет DB, Redis, S3).
- Логи: slog (text в dev, JSON в prod), корреляция по `X-Request-ID`.

## Резервное копирование и отказоустойчивость

- **БД**: ежедневные pg_dump + WAL archiving (настраивается на уровне хостинга).
- **Объекты**: S3 versioning + cross-region replication (конфигурация бакета).
- **Redis**: AOF (`appendonly yes` в compose) для долговечности сессий.
- **Graceful shutdown**: drain in-flight запросов до `HTTP_SHUTDOWN_TIMEOUT`.

## Решения и компромиссы

| Решение | Почему |
|---------|--------|
| **pgx напрямую вместо ORM/GORM** | Контроль SQL, нет скрытых N+1, понятные миграции. |
| **templ + htmx вместо React** | Один язык, нет сборки, SSR из коробки, идеально для CDN. |
| **Сессии в Redis вместо JWT** | Мгновенный logout, отзыв компрометированных сессий. |
| **Web + API в одном бинарнике** | Проще деплой, общие middleware/логи. В проде можно разделить. |
| **Repository без sqlc codegen** | Нет шага генерации, компилируется из коробки. |
| **Stripe no-provider mode** | Локальная разработка без Stripe-аккаунта. |

## Где что искать

| Хочу... | Смотрю... |
|---------|-----------|
| Добавить REST-эндпоинт | `internal/handler/api/` |
| Добавить веб-страницу | `web/views/pages/*.templ` + `internal/handler/web/` |
| Изменить бизнес-правило | `internal/service/` или `internal/billing/` |
| Сменить SQL-запрос | `internal/repository/` |
| Добавить колонку | `migrations/` (новый файл `000003_*.up.sql`) |
| Сменить тариф | сид в `migrations/000002_storage.up.sql` |
| Подключить новый S3 | `internal/storage/s3/s3.go` + конфиг |
