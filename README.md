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

Файл `.env` должен находиться **в корне проекта** (рядом с `go.mod`).

> ⚠️ **Важно про `--env-file .env`.** Команда ниже использует `-f deploy/docker-compose.yml`, а значит Compose по умолчанию ищет `.env` для подстановки `${VAR}` в каталоге `deploy/`, а не в корне. Чтобы значения из корневого `.env` (`S3_PUBLIC_ENDPOINT`, `CORS_ALLOWED_ORIGINS`, `CDN_HOST` и др.) реально попадали в конфигурацию сервисов, **всегда добавляйте `--env-file .env`**. Без него переменные тихо откатываются к дефолтам, рассчитанным на локальную разработку (`http://localhost:9000` и т.п.), и загрузка файлов в проде падает с «failed to fetch».

> **Для первого запуска менять ничего не нужно** — значения по умолчанию рассчитаны на локальную разработку.

#### Шаг 3. Поднять весь стек

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
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

Для запуска в боевых условиях предусмотрен профиль `prod`, который включает веб-сервер Caddy. Caddy автоматически получит SSL/TLS сертификаты для вашего основного домена и любых пользовательских CNAME доменов:

```bash
# 1. Укажите домен
export PUBLIC_BASE_URL=https://pulsar.example.com
export PULSAR_HOST=pulsar.example.com

# 2. Поднимите стек (с профилем prod поднимется Caddy + автоматический TLS)
docker compose --env-file .env -f deploy/docker-compose.yml --profile prod up -d
```

### Production-чеклист

- [ ] Установить `APP_ENV=production`
- [ ] Сгенерировать безопасные `JWT_SECRET` и `CDN_SIGN_KEY`
- [ ] Включить безопасные куки: `SESSION_COOKIE_SECURE=true`, `CSRF_SECURE=true`
- [ ] Настроить реальный SMTP сервер
- [ ] Прописать live-ключи ЮKassa и CryptoBot и зарегистрировать webhooks

---

## 📄 Лицензия

Proprietary. © Pulsar.
