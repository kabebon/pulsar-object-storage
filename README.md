# ☁️ Pulsar — Облачное хранилище файлов

**Production-ready SaaS платформа объектного хранилища с REST API, веб-кабинетом и биллингом.**

Pulsar — это полнофункциональная платформа облачного хранилища, написанная на Go. Поддерживает S3-совместимый API, presigned-загрузку файлов напрямую в хранилище, биллинг через Stripe / ЮKassa / CryptoBot, а также веб-интерфейс на templ + htmx. Весь проект собирается в один Go-бинарник и поднимается одной командой `docker compose up`.

## Стек технологий

```
Go 1.25 · chi (роутер) · pgx (PostgreSQL) · aws-sdk-go-v2 (S3)
Redis · PostgreSQL 16 · MinIO (S3-совместимое хранилище)
templ + htmx (UI) · Stripe · ЮKassa · CryptoBot (биллинг) · Caddy (реверс-прокси, TLS)
Mailpit (локальный SMTP) · Prometheus (метрики) · slog (логирование)
```

---

## 📋 Возможности

| Функция | Описание |
|---------|----------|
| **S3-совместимый API** | AWS SDK, rclone, mc работают без изменений |
| **Presigned uploads** | Клиенты грузят файлы напрямую в S3, минуя приложение |
| **REST API v1** | Полный CRUD бакетов/объектов, API-ключи, метрики использования |
| **Биллинг — три провайдера** | Stripe (международные карты), ЮKassa (карты РФ + СБП), CryptoBot (USDT/TON/BTC) |
| **Тарифные планы** | Free (5 ГБ), Pro (100 ГБ), Business (1 ТБ) — засеиваются в БД автоматически |
| **Безопасность** | bcrypt, Redis-сессии, CSRF, rate-limiting, audit log, RFC 9457 ошибки |
| **Наблюдаемость** | Prometheus `/metrics`, structured logging (slog), health-check `/healthz` |
| **Веб-кабинет** | Регистрация, вход, бакеты, drag-and-drop загрузка, API-ключи, настройки |

---

## 🚀 Запуск проекта

### Системные требования

| Компонент | Минимальная версия | Примечание |
|-----------|--------------------|------------|
| **Docker** | 20.10+ | Обязателен |
| **Docker Compose** | v2 (plugin) | Входит в Docker Desktop |
| **Go** | 1.25+ | Только для локальной разработки без Docker |
| **Git** | Любая | Для клонирования репозитория |

> **Docker Desktop для Windows/Mac** уже включает Docker Compose v2.
> На Linux установите `docker-compose-plugin` отдельно:
> ```bash
> sudo apt install docker-compose-plugin
> ```

---

### Вариант 1: Полный стек через Docker (рекомендуется)

Это самый простой способ запуска — все зависимости поднимаются автоматически.

#### Шаг 1. Клонировать репозиторий

```bash
git clone <url-репозитория> pulsar
cd pulsar
```

#### Шаг 2. Создать файл конфигурации `.env`

```bash
cp deploy/.env.example .env
```

Файл `.env` должен находиться **в корне проекта** (рядом с `go.mod`). Docker Compose читает его оттуда.

> **Для первого запуска менять ничего не нужно** — значения по умолчанию рассчитаны на локальную разработку. Все пароли, порты и endpoint'ы уже настроены на Docker-сервисы.

#### Шаг 3. Поднять весь стек

```bash
docker compose -f deploy/docker-compose.yml up -d --build
```

Эта команда:
1. Поднимет **PostgreSQL 16** (порт 5432)
2. Поднимет **Redis 7** (порт 6379)
3. Поднимет **MinIO** — S3-совместимое хранилище (API: порт 9000, консоль: порт 9001)
4. Запустит **minio-init** — одноразовый контейнер, который создаст бакет `pulsar` в MinIO
5. Поднимет **Mailpit** — локальный SMTP-сервер (SMTP: порт 1025, Web UI: порт 8025)
6. Соберёт и запустит **Pulsar** — само приложение (порт 8080)

> **Первый запуск** занимает 2–5 минут: Docker скачивает образы и компилирует Go-приложение.
> Последующие запуски — несколько секунд.

#### Шаг 4. Проверить, что всё работает

```bash
# Health-check
curl http://localhost:8080/healthz
# Ответ: {"status":"ok"}

# API info
curl http://localhost:8080/api/v1
# Ответ: {"name":"Pulsar API",...}
```

Откройте в браузере: **http://localhost:8080** — вы увидите лендинг Pulsar.

#### Шаг 5. Создать аккаунт и начать работу

1. Откройте **http://localhost:8080/register** и создайте аккаунт
2. Откройте **http://localhost:8025** (Mailpit) — там будет письмо с подтверждением email
3. Нажмите ссылку в письме для верификации
4. Войдите в кабинет на **http://localhost:8080/login**
5. Перейдите в раздел **Бакеты** → создайте бакет
6. Перейдите в раздел **API-ключи** → создайте ключ (secret показывается один раз!)

#### Остановка и перезапуск

```bash
# Остановить все сервисы
docker compose -f deploy/docker-compose.yml down

# Остановить и удалить все данные (PostgreSQL, Redis, MinIO)
docker compose -f deploy/docker-compose.yml down -v

# Посмотреть логи всех сервисов
docker compose -f deploy/docker-compose.yml logs -f --tail=200

# Посмотреть логи только приложения
docker compose -f deploy/docker-compose.yml logs -f pulsar

# Пересобрать только приложение (после изменений кода)
docker compose -f deploy/docker-compose.yml up -d --build pulsar
```

---

### Вариант 2: Локальная разработка (Go + Docker для инфраструктуры)

Если вы хотите вносить изменения в код и быстро перезапускать приложение без пересборки Docker-образа.

#### Шаг 1. Установить Go 1.25+

Скачайте и установите Go с [go.dev/dl](https://go.dev/dl/).

Проверьте:
```bash
go version
# go version go1.25.0 ...
```

#### Шаг 2. Клонировать репозиторий

```bash
git clone <url-репозитория> pulsar
cd pulsar
```

#### Шаг 3. Создать `.env` в корне проекта

```bash
cp deploy/.env.example .env
```

#### Шаг 4. Поднять только инфраструктуру через Docker

```bash
docker compose -f deploy/docker-compose.yml up -d postgres redis minio minio-init mailpit
```

Это запустит PostgreSQL, Redis, MinIO и Mailpit, но **не** само приложение.

#### Шаг 5. Установить зависимости и сгенерировать templ

```bash
# Скачать Go-модули
go mod download

# Сгенерировать Go-код из .templ шаблонов
make templates
# или напрямую:
go run github.com/a-h/templ/cmd/templ generate
```

#### Шаг 6. Запустить приложение

```bash
make run
# или напрямую:
go run ./cmd/pulsar
```

Приложение запустится и выведет в консоль:
```
level=INFO msg="configuration loaded" env=local http_addr=:8080
level=INFO msg="database connected"
level=INFO msg="database migrations applied"
level=INFO msg="redis connected"
level=INFO msg="smtp mailer configured" host=localhost
level=INFO msg="s3 bucket ready" bucket=pulsar
level=INFO msg="server listening" addr=:8080
```

#### Шаг 7. Рабочий цикл разработки

После изменений в коде:
```bash
# 1. Если меняли .templ файлы — регенерировать Go-код:
make templates

# 2. Перезапустить:
make run
```

---

### Сервисы и порты (сводная таблица)

| Сервис | URL / Адрес | Логин | Назначение |
|--------|-------------|-------|------------|
| **Pulsar** (приложение) | http://localhost:8080 | Регистрация через UI | Основной веб-интерфейс и API |
| **MinIO Console** | http://localhost:9001 | `pulsar` / `pulsar12345` | S3-хранилище, управление бакетами |
| **MinIO S3 API** | http://localhost:9000 | `pulsar` / `pulsar12345` | S3-совместимый endpoint |
| **Mailpit Web UI** | http://localhost:8025 | — | Просмотр перехваченных email |
| **Mailpit SMTP** | localhost:1025 | — | Приём писем от приложения |
| **PostgreSQL** | localhost:5432 | `pulsar` / `pulsar` / `pulsar` | Основная БД (user/pass/db) |
| **Redis** | localhost:6379 | — | Сессии, rate-limit, кэш |

---

### Полезные Makefile-команды

Запуск `make help` покажет полный список. Основные:

```bash
# === Разработка ===
make run          # запустить приложение (go run ./cmd/pulsar)
make build        # собрать бинарник в ./bin/pulsar
make templates    # регенерировать templ → Go-код
make tidy         # go mod tidy
make fmt          # форматирование кода (go fmt + gofumpt)
make vet          # статический анализ (go vet)

# === Тестирование ===
make test         # go test -race -count=1 ./...

# === Docker ===
make docker-up    # поднять весь стек (docker compose up -d --build)
make docker-down  # остановить стек
make docker-logs  # логи всех сервисов
make docker-build # собрать только Docker-образ приложения

# === Отладка инфраструктуры ===
make psql         # подключиться к PostgreSQL через psql
make redis-cli    # подключиться к Redis через redis-cli
make minio-mc     # открыть shell с MinIO Client (mc)

# === Миграции ===
make migrate-up   # применить миграции
make migrate-down # откатить последнюю миграцию
```

---

## 📧 SMTP-сервер (Email) — подробно

### Как работает email в Pulsar

Приложение отправляет email в двух случаях:
1. **Верификация email** — при регистрации нового пользователя
2. **Сброс пароля** — при запросе восстановления пароля

Для отправки используется библиотека `go-mail` (`github.com/wneessen/go-mail`), которая подключается к SMTP-серверу по конфигурации из переменных окружения.

### Локальная разработка — Mailpit

В локальной среде используется **Mailpit** — это fake SMTP-сервер, который:
- Принимает **все** письма на любой адрес
- **Никогда не доставляет** их реальным получателям
- Показывает все письма в удобном **веб-интерфейсе**

#### Настройки по умолчанию (из `.env.example`)

```env
SMTP_HOST=localhost        # хост Mailpit (для make run)
SMTP_PORT=1025             # порт SMTP Mailpit
SMTP_USERNAME=             # пустые — Mailpit не требует авторизации
SMTP_PASSWORD=             # пустые
SMTP_FROM=Pulsar <no-reply@pulsar.local>
```

#### Как это работает пошагово

```
┌──────────────┐     SMTP (порт 1025)     ┌──────────────┐
│   Pulsar     │ ────────────────────────→ │   Mailpit    │
│  приложение  │    без TLS, без AUTH      │  контейнер   │
└──────────────┘                           └──────┬───────┘
                                                  │
                                    Web UI (порт 8025)
                                                  │
                                           ┌──────▼───────┐
                                           │   Браузер    │
                                           │ localhost:8025│
                                           └──────────────┘
```

1. Пользователь регистрируется на `http://localhost:8080/register`
2. Pulsar формирует письмо с ссылкой верификации и отправляет через SMTP на порт 1025
3. Mailpit перехватывает письмо и сохраняет его
4. Разработчик открывает `http://localhost:8025` и видит письмо
5. Нажимает ссылку в письме → email подтверждён

#### Нюансы подключения к Mailpit

| Сценарий | SMTP_HOST | Почему |
|----------|-----------|--------|
| `make run` (приложение запущено локально) | `localhost` | Приложение работает на хосте, Mailpit проброшен на `localhost:1025` |
| `docker compose up` (всё в Docker) | `mailpit` | Контейнер `pulsar` обращается к контейнеру `mailpit` по внутреннему имени Docker-сети |

> В `docker-compose.yml` это уже настроено: переменная `SMTP_HOST` переопределена на `mailpit` для контейнера приложения. Менять ничего не нужно.

#### Поведение при отсутствии SMTP

Если `SMTP_HOST` пуст или порт = 0, приложение переключается на **LogMailer** — письма сохраняются только в памяти приложения и выводятся в лог. Это безопасный fallback, но ссылки верификации не будут доступны.

### Продакшн — реальный SMTP-провайдер

Для продакшна замените переменные окружения на реальный SMTP-сервер:

```env
# === Gmail (app password) ===
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USERNAME=your@gmail.com
SMTP_PASSWORD=xxxx-xxxx-xxxx-xxxx    # App Password (не основной пароль!)
SMTP_FROM=Pulsar <no-reply@yourdomain.com>

# === SendGrid ===
SMTP_HOST=smtp.sendgrid.net
SMTP_PORT=587
SMTP_USERNAME=apikey
SMTP_PASSWORD=SG.xxxxxxxxxxxxxxxxxxxx
SMTP_FROM=Pulsar <no-reply@yourdomain.com>

# === Mailgun ===
SMTP_HOST=smtp.mailgun.org
SMTP_PORT=587
SMTP_USERNAME=postmaster@yourdomain.com
SMTP_PASSWORD=your-mailgun-password
SMTP_FROM=Pulsar <no-reply@yourdomain.com>
```

> ⚠️ **Важно!** В текущей реализации TLS-политика установлена на `gomail.NoTLS` в файле `internal/mailer/mailer.go` (строка 73). Для продакшна с реальным SMTP-провайдером (порт 587/STARTTLS) **необходимо** изменить на:
> ```go
> gomail.WithTLSPolicy(gomail.TLSOpportunistic)
> // или для строгого TLS:
> gomail.WithTLSPolicy(gomail.TLSMandatory)
> ```

---

## ⚠️ Известные проблемы и что нужно изменить

### 🔴 Не работает загрузка (push) файлов в бакет

Загрузка файлов через веб-интерфейс (drag-and-drop в разделе Бакеты) **не работает**.

**Что происходит:** presigned PUT URL генерируется на бэкенде, но фактический push файла из браузера в S3/MinIO не выполняется корректно.

**Цепочка загрузки файла (для отладки):**
```
1. Пользователь перетаскивает файл в UI
2. JS отправляет POST /api/v1/buckets/{id}/objects/presign-upload
3. Бэкенд генерирует presigned PUT URL через aws-sdk-go-v2
4. JS отправляет PUT-запрос с файлом на presigned URL (напрямую в MinIO)
5. JS отправляет POST /api/v1/buckets/{id}/objects/confirm
6. Бэкенд сохраняет метаданные объекта в PostgreSQL
```

**Файлы для отладки:**
- `internal/storage/s3/s3.go` — генерация presigned URL (`PresignUpload`)
- `internal/service/` — бизнес-логика загрузки
- `internal/handler/api/` — API endpoint'ы
- `web/static/js/` — JavaScript на фронтенде
- `web/views/pages/buckets.templ` — шаблон страницы бакетов

### 🟡 Изменить надпись «С возвращением» на странице логина

На странице входа (`/login`) отображается заголовок **«С возвращением»** — нужно заменить на другой текст.

**Файл:** `web/views/pages/auth.templ`, строка 17:
```go
// Было:
@authCard("С возвращением", "Войдите в свой кабинет", p) {

// Заменить "С возвращением" на нужный текст, например:
@authCard("Вход в аккаунт", "Войдите в свой кабинет", p) {
```

**После изменения `.templ`-файла необходимо регенерировать Go-код:**
```bash
make templates
```
Это перегенерирует файл `auth_templ.go` из `auth.templ`.

---

## 📝 Что нужно доработать (TODO)

### Критичное
- [ ] Исправить загрузку файлов в бакет (presigned upload flow)
- [ ] Изменить надпись «С возвращением» на странице логина
- [ ] Изменить TLS-политику SMTP на `TLSOpportunistic` / `TLSMandatory` для продакшна

### Биллинг — подключить провайдеры
- [ ] **Stripe** — заполнить `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET` и price IDs; зарегистрировать webhook URL `POST /webhooks/stripe`
- [ ] **ЮKassa** — заполнить `YOOKASSA_SHOP_ID` + `YOOKASSA_SECRET_KEY`; зарегистрировать webhook URL `POST /webhooks/yookassa`; уточнить цены в рублях в `planPriceRub()` в `internal/handler/web/billing.go`
- [ ] **CryptoBot** — создать приложение в @CryptoBot (`/pay → Create App`), заполнить `CRYPTOBOT_TOKEN`; зарегистрировать webhook URL `POST /webhooks/cryptobot`; прописать реальные курсы в `planCryptoAmount()` (или подключить live rate API)

### Улучшения
- [ ] Настроить бэкапы PostgreSQL (pg_dump + WAL archiving)
- [ ] Подключить Prometheus / Grafana к эндпоинту `/metrics`
- [ ] Сгенерировать продакшн-секреты (`JWT_SECRET`, `CDN_SIGN_KEY`)
- [ ] Включить `SESSION_COOKIE_SECURE=true` и `CSRF_SECURE=true` за HTTPS
- [ ] Добавить GitHub Actions для CI/CD (тесты, линтинг, сборка Docker-образа)
- [ ] Подключить live exchange rate API для криптовалютных платежей (CryptoBot)

---

## 📖 Конфигурация

Все настройки задаются через **переменные окружения**. Полный пример с комментариями: `deploy/.env.example`.

### Все переменные окружения

#### Приложение

| Переменная | По умолчанию | Описание |
|-----------|--------------|----------|
| `APP_ENV` | `local` | Режим: `local` / `production` / `test` |
| `APP_NAME` | `Pulsar` | Название приложения |
| `HTTP_ADDR` | `:8080` | Адрес и порт HTTP-сервера |
| `PUBLIC_BASE_URL` | `http://localhost:8080` | Внешний URL (для ссылок в письмах, Stripe redirect) |
| `LOG_LEVEL` | `info` | Уровень логирования: `debug` / `info` / `warn` / `error` |

#### База данных (PostgreSQL)

| Переменная | По умолчанию | Описание |
|-----------|--------------|----------|
| `DATABASE_URL` | `postgres://pulsar:pulsar@localhost:5432/pulsar?sslmode=disable` | DSN подключения |
| `DB_MAX_OPEN_CONNS` | `25` | Максимум открытых соединений |
| `DB_MAX_IDLE_CONNS` | `5` | Максимум idle-соединений |
| `DB_CONN_MAX_LIFETIME` | `1h` | Время жизни соединения |

#### Redis

| Переменная | По умолчанию | Описание |
|-----------|--------------|----------|
| `REDIS_ADDR` | `localhost:6379` | Адрес Redis |
| `REDIS_PASSWORD` | (пусто) | Пароль Redis |
| `REDIS_DB` | `0` | Номер БД Redis |

#### S3 / MinIO

| Переменная | По умолчанию | Описание |
|-----------|--------------|----------|
| `S3_ENDPOINT` | `http://localhost:9000` | S3-совместимый endpoint |
| `S3_REGION` | `us-east-1` | Регион |
| `S3_ACCESS_KEY` | `pulsar` | Ключ доступа |
| `S3_SECRET_KEY` | `pulsar12345` | Секретный ключ |
| `S3_BUCKET` | `pulsar` | Имя бакета |
| `S3_USE_PATH_STYLE` | `true` | Path-style доступ (обязательно для MinIO) |
| `S3_PRESIGN_EXPIRY` | `15m` | Время жизни presigned URL |
| `S3_PUBLIC_URL_PREFIX` | `http://localhost:9000/pulsar` | Публичный URL бакета |

#### Безопасность

| Переменная | По умолчанию | Описание |
|-----------|--------------|----------|
| `JWT_SECRET` | `change-me-...` | Секрет подписи JWT (≥32 байта в prod!) |
| `SESSION_COOKIE_NAME` | `pulsar_sid` | Имя cookie сессии |
| `SESSION_TTL` | `168h` | Время жизни сессии (7 дней) |
| `SESSION_COOKIE_SECURE` | `false` | `true` для HTTPS в продакшне |
| `CSRF_SECURE` | `false` | `true` для HTTPS в продакшне |

#### Email (SMTP)

| Переменная | По умолчанию | Описание |
|-----------|--------------|----------|
| `SMTP_HOST` | `localhost` | SMTP-сервер |
| `SMTP_PORT` | `1025` | Порт SMTP |
| `SMTP_USERNAME` | (пусто) | Логин SMTP (пусто = без AUTH) |
| `SMTP_PASSWORD` | (пусто) | Пароль SMTP |
| `SMTP_FROM` | `Pulsar <no-reply@localhost>` | Адрес отправителя |

#### Stripe (биллинг)

| Переменная | По умолчанию | Описание |
|-----------|--------------|----------|
| `STRIPE_SECRET_KEY` | (пусто) | Stripe API ключ. Пусто = no-provider mode |
| `STRIPE_WEBHOOK_SECRET` | (пусто) | Секрет для проверки подписи webhook |
| `STRIPE_PRICE_MONTHLY_PRO` | (пусто) | Price ID для Pro (месяц) |
| `STRIPE_PRICE_YEARLY_PRO` | (пусто) | Price ID для Pro (год) |
| `STRIPE_PRICE_MONTHLY_BUSINESS` | (пусто) | Price ID для Business (месяц) |
| `STRIPE_PRICE_YEARLY_BUSINESS` | (пусто) | Price ID для Business (год) |

#### CDN

| Переменная | По умолчанию | Описание |
|-----------|--------------|----------|
| `CDN_DEFAULT_DOMAIN` | `cdn.localhost` | CNAME-target для кастомных доменов |
| `CDN_SIGN_KEY` | `change-me-...` | HMAC-ключ для подписи CDN URL |

### No-provider mode (Stripe)

Если `STRIPE_SECRET_KEY` пуст — биллинг работает в **демо-режиме**: страница тарифов отображается, квоты считаются, но Stripe Checkout и Customer Portal отключены. Это позволяет разрабатывать и тестировать без Stripe-аккаунта.

---

## 🏗 Архитектура проекта

Подробная документация: `docs/ARCHITECTURE.md`.

### Принципы

- **Многослойная архитектура**: handler → service → repository → storage
- **Никакой ORM** — чистый pgx с SQL-запросами
- **Templ + htmx** для UI (server-side rendering + minimal JavaScript)
- **Structured logging** через slog
- **Graceful shutdown** с таймаутом 30 секунд

### Структура каталогов

```
pulsar/
├── cmd/pulsar/                 # Точка входа, DI (dependency injection)
│   └── main.go                 # Инициализация всех компонентов и запуск сервера
│
├── internal/                   # Вся бизнес-логика (не экспортируется)
│   ├── config/                 # Загрузка конфигурации из env-переменных
│   │   └── config.go
│   ├── server/                 # Сборка HTTP-сервера, middleware-стек, graceful shutdown
│   ├── middleware/             # auth, csrf, rate-limit, metrics, recover
│   ├── handler/
│   │   ├── web/               # Веб-страницы (htmx, templ-рендеринг)
│   │   └── api/               # REST API v1 (JSON, RFC 9457 ошибки)
│   ├── service/               # Бизнес-логика: auth, storage, api-keys
│   ├── billing/               # Stripe: Checkout, Portal, webhooks
│   ├── domain/                # DNS-верификация кастомных доменов
│   ├── repository/            # SQL-запросы (pgx), управление миграциями
│   ├── storage/s3/            # S3-адаптер: presigned URLs, CRUD объектов
│   ├── cache/                 # Redis: сессии, rate-limit
│   ├── mailer/                # SMTP (go-mail): отправка email
│   ├── models/                # Доменные типы (User, Bucket, Object и т.д.)
│   ├── errors/                # Кастомные типы ошибок
│   └── templ/                 # Сгенерированные templ-компоненты
│
├── web/
│   ├── views/                 # Шаблоны templ
│   │   ├── layouts/           # Base layout (header, footer, nav)
│   │   ├── pages/             # Страницы (auth, buckets, billing, settings...)
│   │   └── partials/          # Переиспользуемые компоненты
│   └── static/                # Статика
│       ├── css/               # Стили
│       ├── js/                # JavaScript (htmx, upload logic)
│       └── img/               # Изображения, favicon
│
├── migrations/                # SQL-миграции (golang-migrate)
│   ├── 000001_init_schema.up.sql     # users, email_verifications, audit_log
│   ├── 000001_init_schema.down.sql
│   ├── 000002_storage.up.sql         # buckets, objects, api_keys, plans, subscriptions, domains
│   ├── 000002_storage.down.sql
│   └── embed.go               # go:embed для миграций
│
├── deploy/                    # Инфраструктура
│   ├── docker-compose.yml     # Все сервисы: postgres, redis, minio, mailpit, pulsar, caddy
│   ├── Dockerfile             # Multi-stage: builder (Go + templ) → runtime (alpine)
│   ├── Caddyfile              # Реверс-прокси + on-demand TLS для кастомных доменов
│   └── .env.example           # Пример всех переменных окружения
│
├── docs/
│   ├── ARCHITECTURE.md        # Подробная архитектурная документация
│   └── openapi.yaml           # OpenAPI 3 спецификация REST API
│
├── Makefile                   # Все команды разработки
├── go.mod / go.sum            # Go-модули
└── .gitignore
```

### Схема БД (основные таблицы)

```
users                    # Пользователи (email, password_hash, status)
  ├── email_verifications  # Токены верификации/сброса пароля
  ├── audit_log            # Лог всех действий
  ├── buckets              # Бакеты пользователя
  │   ├── objects          # Объекты (файлы) в бакете
  │   └── custom_domains   # Кастомные домены бакета
  ├── api_keys             # API-ключи
  ├── subscriptions        # Подписка на тариф → plans
  └── usage_events         # Учёт использования (storage, bandwidth, api_calls)

plans                    # Тарифные планы (free, pro, business)
```

### Тарифные планы (засеиваются при первой миграции)

| План | Хранилище | Трафик/мес | Бакетов | Цена/мес |
|------|-----------|-----------|---------|----------|
| **Free** | 5 ГБ | 50 ГБ | 3 | $0 |
| **Pro** | 100 ГБ | 1 ТБ | 50 | $99 |
| **Business** | 1 ТБ | 10 ТБ | ∞ | $499 |

---

## 🧪 Тестирование

```bash
# Запустить все тесты с race-детектором
make test

# То же самое напрямую
go test -race -count=1 ./...

# Тесты с покрытием для конкретных пакетов
go test -cover ./internal/service/ ./internal/domain/ ./internal/cache/
```

Покрытие сфокусировано на критичных путях: хеширование паролей, валидация бакетов/доменов, генерация токенов, кодирование сессий.

---

## 📤 Публикация на GitHub

### Шаг 1. Создать репозиторий на GitHub

**Вариант А — через GitHub CLI:**
```bash
# Установить gh: https://cli.github.com/
gh repo create pulsar --private --source=. --push
# Готово! Репозиторий создан и код запушен.
```

**Вариант Б — вручную через github.com:**
1. Зайдите на https://github.com/new
2. Введите имя репозитория (например, `pulsar`)
3. Выберите **Private**
4. **НЕ** ставьте галочки «Add README», «Add .gitignore» — у нас уже всё есть
5. Нажмите **Create repository**

### Шаг 2. Инициализировать Git (если ещё не инициализирован)

```bash
cd pulsar

# Инициализация
git init
git branch -M main
```

### Шаг 3. Добавить удалённый репозиторий

```bash
# HTTPS (рекомендуется для начала):
git remote add origin https://github.com/<ваш-username>/pulsar.git

# Или SSH (если настроен SSH-ключ):
git remote add origin git@github.com:<ваш-username>/pulsar.git
```

### Шаг 4. Первый коммит и push

```bash
# Добавить все файлы
git add .

# Проверить, что .env НЕ в списке (должен быть в .gitignore)
git status

# Создать коммит
git commit -m "Initial commit: Pulsar cloud storage platform"

# Запушить
git push -u origin main
```

### Шаг 5. Проверить

Откройте `https://github.com/<ваш-username>/pulsar` — все файлы проекта должны быть там.

### Что НЕ попадёт в репозиторий

Уже настроено в `.gitignore`:

| Файл/папка | Причина |
|------------|---------|
| `.env`, `.env.local` | Секреты (пароли, ключи API) |
| `/bin/` | Скомпилированные бинарники |
| `*.exe`, `*.dll`, `*.so` | Бинарные файлы |
| `/data/`, `/pgdata/` | Локальные Docker-тома |
| `.idea/`, `.vscode/` | Настройки IDE |
| `coverage.*` | Артефакты тестов |

> ⚠️ **Важно!** Перед первым push убедитесь, что файл `.env` **не попал** в git:
> ```bash
> git status          # .env не должен быть в списке
> git ls-files .env   # должно быть пусто
> ```
> Если `.env` уже был закоммичен ранее, удалите из отслеживания:
> ```bash
> git rm --cached .env
> git commit -m "Remove .env from tracking"
> ```

---

## 🔧 Production-деплой

### С Caddy (автоматический HTTPS)

```bash
# 1. Настроить домен в .env
PUBLIC_BASE_URL=https://pulsar.example.com

# 2. Поднять с prod-профилем (включает Caddy)
docker compose -f deploy/docker-compose.yml --profile prod up -d
```

Caddy автоматически получит TLS-сертификат от Let's Encrypt для вашего домена. Для кастомных доменов пользователей используется on-demand TLS через ask-эндпоинт `/api/v1/domains/verify-tls`.

### Production-чеклист

- [ ] `APP_ENV=production`
- [ ] `JWT_SECRET` — сгенерировать: `openssl rand -hex 32`
- [ ] `CDN_SIGN_KEY` — сгенерировать: `openssl rand -hex 32`
- [ ] `SESSION_COOKIE_SECURE=true`
- [ ] `CSRF_SECURE=true`
- [ ] `STRIPE_SECRET_KEY` и `STRIPE_WEBHOOK_SECRET` — live mode ключи
- [ ] Реальный SMTP-сервер + изменить TLS-политику в `mailer.go`
- [ ] Бакет S3 с versioning + lifecycle policies
- [ ] Внешний CDN (Cloudflare / CloudFront)
- [ ] Бэкапы PostgreSQL (pg_dump + WAL archiving)
- [ ] Мониторинг: Prometheus scrape `/metrics`
- [ ] Настроить `TRUSTED_PROXIES` если за балансировщиком

---

## 📄 Лицензия

Proprietary. © Pulsar.
