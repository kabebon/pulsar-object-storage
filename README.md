# ☁️ Pulsar — Облачное хранилище файлов

**Production-ready SaaS платформа объектного хранилища с REST API, веб-кабинетом и биллингом.**

Pulsar — это полнофункциональная платформа облачного хранилища, написанная на Go. Поддерживает S3-совместимый API, presigned-загрузку файлов напрямую в хранилище, биллинг через ЮKassa и CryptoBot, а также веб-интерфейс на templ + htmx. Весь проект собирается в один Go-бинарник и поднимается одной командой `docker compose up`.

## Стек технологий

```text
Go 1.25 · chi (роутер) · pgx (PostgreSQL) · aws-sdk-go-v2 (S3)
Redis · PostgreSQL 16 · MinIO (S3-совместимое хранилище)
templ + htmx (UI) · ЮKassa · CryptoBot (биллинг) · Caddy (реверс-прокси, TLS)
Mailpit (локальный SMTP) · Prometheus (метрики) · slog (логирование)
```

---

## 📋 Возможности

| Функция | Описание |
|---------|----------|
| **S3-совместимый API** | AWS SDK, rclone, mc работают без изменений |
| **Presigned uploads** | Клиенты грузят файлы напрямую в S3, минуя приложение |
| **REST API v1** | Полный CRUD бакетов/объектов, API-ключи, метрики использования |
| **Биллинг** | ЮKassa (карты РФ + СБП), CryptoBot (USDT/TON/BTC) |
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
> На Linux установите `docker-compose-plugin` отдельно.

---

### Вариант 1: Полный стек через Docker (локальная разработка, рекомендуется)

Это самый простой способ запуска — все зависимости поднимаются автоматически. По умолчанию всё настроено для работы на `localhost`.

#### Шаг 1. Клонировать репозиторий

```bash
git clone <url-репозитория> pulsar
cd pulsar
```

#### Шаг 2. Создать файл конфигурации `.env`

```bash
cp deploy/.env.example .env
```

Файл `.env` должен находиться **в корне проекта** (рядом с `go.mod`).

> ⚠️ **Критично про `--env-file .env`.** Команда ниже использует `-f deploy/docker-compose.yml`, а значит Compose по умолчанию ищет `.env` для подстановки `${VAR}` в каталоге `deploy/`, а не в корне. Чтобы значения из корневого `.env` реально попадали в конфигурацию сервисов, **всегда добавляйте `--env-file .env`**. Без него переменные тихо откатываются к дефолтам, рассчитанным на локальную разработку (`http://localhost:9000` и т.п.), и в проде загрузка файлов падает с «failed to fetch».

> **Для первого локального запуска менять ничего не нужно** — значения по умолчанию рассчитаны на `localhost`.

#### Шаг 3. Поднять весь стек

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
# или: make docker-up
```

#### Шаг 4. Проверить, что всё работает

```bash
# Health-check
curl http://localhost:8080/healthz
# Ответ: {"status":"ok"}
```

Откройте в браузере: **http://localhost:8080** — вы увидите лендинг Pulsar.

#### Шаг 5. Создать аккаунт и начать работу

1. Откройте **http://localhost:8080/register** и создайте аккаунт
2. Откройте **http://localhost:8025** (Mailpit) — там будет письмо с подтверждением email
3. Нажмите ссылку в письме для верификации
4. Войдите в кабинет на **http://localhost:8080/login**
5. Создавайте бакеты, загружайте файлы и управляйте ключами API.

> Для локального запуска MinIO (API `:9000`, консоль `:9001`) проброшены на `127.0.0.1`, поэтому presigned-загрузки работают «из коробки».

---

### Вариант 2: Локальная разработка (Go + Docker для инфраструктуры)

#### Шаг 1. Поднять только инфраструктуру через Docker

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d postgres redis minio minio-init mailpit
```

#### Шаг 2. Сгенерировать шаблоны и запустить

```bash
make templates
make run
```

---

## 📁 Загрузка файлов (как это работает)

Pulsar использует **presigned-схему**: браузер грузит файл **напрямую в хранилище** (MinIO), минуя приложение. Так файловые байты не ложатся на сервер и не тратят его трафик.

Цепочка из трёх запросов (см. `web/static/js/uploader.js`):

1. `POST /app/buckets/{id}/objects/presign-upload` — приложение подписывает URL и отдаёт его браузеру.
2. `PUT https://<storage-host>/pulsar/...` — браузер грузит файл напрямую в MinIO по подписанному URL.
3. `POST /app/buckets/{id}/objects/confirm` — приложение записывает метаданные (размер, тип) в БД.

Шаг 2 — кросс-доменный (домен приложения → домен хранилища), поэтому требует двух вещей:
- **CORS** на стороне хранилища (`MINIO_API_CORS_ALLOW_ORIGIN`).
- **Публичной доступности** домена хранилища для браузера (`S3_PUBLIC_ENDPOINT` + DNS).

### В локальной разработке

Работает из коробки: MinIO проброшен на `127.0.0.1:9000`, CORS разрешает `localhost:8080`.

### В продакшене (за Caddy)

MinIO закрыт внутри Docker-сети, поэтому браузер до него не достучится напрямую. Решение — отдельный поддомен хранения, который Caddy проксирует на `minio:9000` (см. `deploy/Caddyfile`, блок `{$CDN_HOST}`). Все настройки задаются в `.env` (см. «Production-деплой» ниже).

### Если «failed to fetch» в проде

Это частый симптом при переносе на прод. Падает шаг 2 (`PUT`). Чеклист по порядку:

1. **DNS.** Проверьте, что `<CDN_HOST>` резолвится во внешний IP сервера: `getent hosts cdn.example.com`.
2. **`.env` + `--env-file .env`.** Убедитесь, что в `.env` заданы `S3_PUBLIC_ENDPOINT`, `CORS_ALLOWED_ORIGINS`, `CDN_HOST`, и что команда запуска содержит `--env-file .env` (иначе переменные не интерполируются — см. предупреждение выше).
3. **Контейнеры пересозданы.** Переменные читаются только при создании контейнера. После правки `.env`: `docker compose ... up -d --force-recreate pulsar minio`.
4. **TLS-сертификат Caddy.** Проверьте `logs caddy` на ошибки ACME. При первом запуске Caddy получает сертификат автоматически, но DNS уже должен резолвиться.
5. **CORS на MinIO.** Preflight-проверка:
   ```bash
   curl -ski -X OPTIONS https://<CDN_HOST>/pulsar/ \
     -H "Origin: https://<PULSAR_HOST>" \
     -H "Access-Control-Request-Method: PUT" \
     -D - -o /dev/null | grep -i access-control
   ```
   Должны быть строки `access-control-allow-origin: https://<PULSAR_HOST>`. Если их нет — `CORS_ALLOWED_ORIGINS` не подхватился MinIO (см. п. 2–3).

---

## 📧 SMTP-сервер (Email)

Для локальной разработки используется **Mailpit** (fake SMTP на порту 1025). Все отправленные письма можно посмотреть в веб-интерфейсе по адресу `http://localhost:8025`. 

Для продакшна требуется указать реальные реквизиты в конфигурации (например, Amazon SES, SendGrid или Mailgun). Приложение автоматически включает TLS для портов отличных от локальных тестовых.

---

## 📖 Конфигурация

Все настройки задаются через **переменные окружения**. Полный пример: `deploy/.env.example`.

### Основные переменные базы и приложения

Для контейнера мы передаем конфигурацию БД отдельными переменными:
- `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSLMODE`

Остальные настройки: `HTTP_ADDR`, `REDIS_ADDR`, ключи MinIO (`S3_ENDPOINT`, `S3_ACCESS_KEY` и т.д.).

### Биллинг

| Переменная | По умолчанию | Описание |
|-----------|--------------|----------|
| `YOOKASSA_SHOP_ID` | (пусто) | ID магазина ЮKassa. Пусто = демо-режим |
| `YOOKASSA_SECRET_KEY` | (пусто) | Секретный ключ ЮKassa |
| `CRYPTOBOT_TOKEN` | (пусто) | API-токен из @CryptoBot. Пусто = демо-режим |
| `CRYPTOBOT_NETWORK` | `mainnet` | `mainnet` или `testnet` |

Если переменные не заданы, провайдеры работают в **демо-режиме**: пользователи видят тарифы и лимиты, но не могут совершать реальные платежи.

---

## 🏗 Архитектура проекта

Подробная документация: `docs/ARCHITECTURE.md`.

- **Многослойная архитектура**: handler → service → repository → storage
- **Никакой ORM** — чистый pgx с SQL-запросами
- **Templ + htmx** для UI (server-side rendering)

---

## 🔧 Production-деплой

Для запуска в боевых условиях предусмотрен профиль `prod`, который включает веб-сервер Caddy с автоматическим HTTPS (Let's Encrypt). Caddy терминирует TLS для двух доменов:

- **`{$PULSAR_HOST}`** (домен приложения) → `pulsar:8080`
- **`{$CDN_HOST}`** (домен хранения) → `minio:9000` — чтобы браузер мог грузить/скачивать файлы напрямую по presigned-ссылкам.

### Шаг 1. DNS

Добавьте две A-записи, обе указывающие на IP сервера:

| Домен | Назначение |
|-------|-----------|
| `<your-domain>` (напр. `pulsar.example.com`) | Приложение |
| `cdn.<your-domain>` (напр. `cdn.pulsar.example.com`) | Хранилище (MinIO) |

Без DNS-записи для `cdn.` Caddy не сможет выпустить TLS-сертификат, и загрузка файлов не заработает.

### Шаг 2. Настроить `.env`

Скопируйте `deploy/.env.example` в `.env` и задайте продакшен-значения (минимальный набор для рабочей загрузки файлов):

```bash
APP_ENV=production
PUBLIC_BASE_URL=https://pulsar.example.com

# Домены для Caddy
PULSAR_HOST=pulsar.example.com
CDN_HOST=cdn.pulsar.example.com

# Хранилище: домен, по которому БРАУЗЕР обращается к MinIO (через Caddy)
S3_PUBLIC_ENDPOINT=https://cdn.pulsar.example.com

# CORS: origin домена приложения, с которого браузер шлёт PUT в MinIO
CORS_ALLOWED_ORIGINS=https://pulsar.example.com

# Безопасные куки (трафик за Caddy = HTTPS)
SESSION_COOKIE_SECURE=true
CSRF_SECURE=true

# Сгенерируйте случайные секреты длиной от 32 байт
JWT_SECRET=<openssl rand -base64 48>
```

Полный список переменных и комментарии — в `deploy/.env.example`.

### Шаг 3. Поднять стек

```bash
docker compose --env-file .env -f deploy/docker-compose.yml --profile prod up -d --build
# или: make docker-up-prod
```

> ⚠️ `--env-file .env` обязателен — иначе переменные из корневого `.env` не подставятся в конфигурацию сервисов.

### Production-чеклист

- [ ] Установить `APP_ENV=production`
- [ ] Добавить DNS A-записи для `PULSAR_HOST` и `CDN_HOST`
- [ ] Задать `S3_PUBLIC_ENDPOINT=https://<CDN_HOST>` и `CORS_ALLOWED_ORIGINS=https://<PULSAR_HOST>`
- [ ] Сгенерировать безопасные `JWT_SECRET` (от 32 байт)
- [ ] Включить безопасные куки: `SESSION_COOKIE_SECURE=true`, `CSRF_SECURE=true`
- [ ] Настроить реальный SMTP сервер
- [ ] Прописать live-ключи ЮKassa и CryptoBot и зарегистрировать webhooks
- [ ] Проверить preflight-CORS (см. раздел «Загрузка файлов»)

---

## 📄 Лицензия

Proprietary. © Pulsar.
