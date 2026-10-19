# Time Leak Backend

Go backend with SQLite, JWT auth, phone-only WhatsApp OTP auth through WHAPI, notes, and ads rotation.

## Features

- API versioning under `/api/v1`
- Existing user/profile/notes endpoints preserved
- Notes support multipart uploads with up to `5` files per note
- Notes have independent text/audio titles, one-time completion and yearly/monthly/weekly recurrence
- Expiring note shares include public JSON, Open Graph previews and signed file downloads
- Uploaded note files are stored under `./note_files/{audio|photo|video|document}`
- Access token TTL is exactly `60s`
- Refresh token rotation with revocation
- OTP auth (phone-only WhatsApp via WHAPI):
  - `POST /api/v1/auth/otp/request`
  - `POST /api/v1/auth/otp/verify`
  - `POST /api/v1/auth/register`
  - `POST /api/v1/auth/login`
  - `POST /api/v1/auth/password-reset/otp/request`
  - `POST /api/v1/auth/password-reset/otp/verify`
  - `POST /api/v1/auth/password-reset/confirm`
  - OTP stored hashed (HMAC-SHA256), 4-digit code, TTL `3-5m`
  - rate limit + attempts lockout
- Admin auth:
  - `POST /api/v1/admin/auth/login`
  - hardcoded defaults: `Admin` / `QRT123` (override with env)
- Ads engine:
  - Admin CRUD under `/api/v1/admin/ads`
  - Client rotation endpoint `GET /api/v1/ads/next`
- Swagger UI: `GET /swagger`
- Note file download route: `GET /api/v1/note-files/{path}`

## Run

```bash
make run
```

Audio titles require `ffmpeg` and `ffprobe` on the server PATH (Ubuntu/Debian:
`sudo apt-get install ffmpeg`; macOS: `brew install ffmpeg`). Ordinary notes and
existing uploads continue to work without them; audio-title uploads return 503
if the tools are unavailable. Executable paths can be configured below.

Share links use `https://timeleak.kz/share/{token}`. The public domain must route
`/share/*`, the legacy `/s/*`, `/api/v1/notes/shared/*` (including signed files
and previews), and both app association files to this backend. Other routes
can continue serving the admin site. The production Caddy configuration is in
`deploy/Caddyfile`; install it as `/etc/caddy/Caddyfile`, validate with
`caddy validate --config /etc/caddy/Caddyfile`, and reload Caddy. Restart the
backend after changing `.env`.

Environment overrides:

- `APP_ADDR` (default `:8081`)
- `DB_PATH` (default `data`)
- `DB_NAME` (default `timeleak.db`)
- `NOTE_FILES_PATH` (default `./note_files`)
- `PUBLIC_BASE_URL` (default `https://timeleak.kz`; public share pages and signed file URLs; reverse proxy this domain to the backend)
- `APPLE_APP_ID` (default `9YQXS629VS.kz.timeleak.project.app`; app ID prefix + bundle ID for Universal Links)
- `APPLE_APP_STORE_ID` (default `6762095094`; numeric App Store ID for the iOS fallback)
- `ANDROID_PACKAGE_NAME` (default `kz.timeleak.project.app`; App Links and Google Play fallback)
- `ANDROID_SHA256_CERT_FINGERPRINTS` (comma-separated SHA256 signing certificate fingerprints; required to serve Android associations)
- `SHARE_SIGNING_SECRET` (optional separate HMAC secret; falls back to `JWT_ACCESS_SECRET`)
- `NOTE_FFMPEG_PATH` / `NOTE_FFPROBE_PATH` (default `ffmpeg` / `ffprobe`)
- `JWT_ACCESS_SECRET`
- `JWT_ADMIN_SECRET`
- `JWT_REFRESH_TTL_HOURS`
- `OTP_HMAC_SECRET`
- `OTP_REQUEST_COOLDOWN_SEC`
- `OTP_MAX_ATTEMPTS`
- `OTP_LOCK_DURATION_SEC`
- `OTP_EXPIRES_IN_SEC` (must be between `180` and `300`)
- `WHAPI_BASE_URL` (default `https://whapi.kz`)
- `WHAPI_TOKEN`
- `WHAPI_TIMEOUT` (default `10s`)
- `RESEND_API_KEY` (server-side email sending key)
- `RESEND_FROM_EMAIL` (default `noreply@timeleak.kz`)
- `RESEND_FROM_NAME` (default `Time Leak`)
- `OTP_RESEND_COOLDOWN` (email resend delay, default `32s`)
- `RATE_OTP_REQUEST_PER_DAY` (email send attempts per address over a rolling 24 hours, default `10`)
- `OTP_TEST_ENABLED` (`false` by default; ignored in production)
- `OTP_TEST_PHONE`
- `OTP_TEST_CODE`
- `ADMIN_USERNAME`
- `ADMIN_PASSWORD`
- `ENABLE_TESTING_ENDPOINTS` (`false` by default)

## Migrations

Migrations are embedded and applied automatically at startup from:

- `traits/database/migrations/0001_initial.sql`
- `traits/database/migrations/0002_users_phone_column.sql`
- `traits/database/migrations/0003_users_phone_index.sql`
- `traits/database/migrations/0004_otp_requests.sql`
- `traits/database/migrations/0005_ads.sql`
- `traits/database/migrations/0006_refresh_tokens_auth_type.sql`
- `traits/database/migrations/0007_refresh_tokens_role.sql`
- `traits/database/migrations/0008_auth_verifications.sql`
- `traits/database/migrations/0009_note_files.sql`
- `0010`–`0015`: admin refresh tokens, account activity, auth audit, token hashes, analytics indexes
- `0016`–`0019`: note calendar date, date source, file count and file type
- `0020_notes_metadata_backfill.sql`: transactional Go backfill and the calendar query index
- `0021_email_otp_send_attempts.sql`: persistent email send cooldown and daily quota
- `0022_note_features.sql`: nullable titles, separate audio title, lifecycle, recurrence, completion and cascading share tokens

No manual migrate command is required.

Before deploying, take a consistent SQLite backup (including pending WAL data,
for example with SQLite's backup API) and retain `note_files`. Migrations run at
startup. Migration 0020 preserves existing IDs, owners, titles, file paths and
creation timestamps. It derives file metadata and uses the UTC creation day as
the fallback calendar date, marked `note_date_source: "created_at"`. It does not
guess a selected day from a title. Invalid legacy creation timestamps abort the
backfill instead of silently losing calendar entries. Already-applied SQL files
must not be edited.

## Swagger

- OpenAPI JSON: `GET /swagger.json`
- Swagger UI: `GET /swagger`

The canonical OpenAPI 3.0.3 document is
[`internal/handler/openapi.json`](internal/handler/openapi.json), embedded into
the binary and served unchanged at `/swagger.json`. Edit that JSON to update
the contract. The **Notes** tag contains the Flutter workflow, named response
models, request/response examples, Bearer authentication and error responses.
Import `/swagger.json` into API/client-generation tools after deployment.

The short Flutter handoff for titles, recurrence, audio and sharing is
[`docs/FLUTTER_FEATURES_HANDOFF_RU.md`](docs/FLUTTER_FEATURES_HANDOFF_RU.md).
The existing complete API guide remains at
[`docs/FLUTTER_API_GUIDE_RU.md`](docs/FLUTTER_API_GUIDE_RU.md).

One-time notes are marked complete at startup and daily at 00:05 UTC when
`note_date` is before the current UTC day. They remain available for history.
Recurring notes are never completed automatically. List and day queries return
virtual `occurrences` while keeping the original note ID, date and attachments.

## OTP: email и WhatsApp

Оба канала доступны независимо. Для одного подтверждения пользователь выбирает,
куда получить код. Ключи Resend и WHAPI остаются в `.env` на сервере; клиент их
не передаёт. Текущие телефонные маршруты сохранены:

- `POST /api/v1/auth/otp/request` — отправить код в WhatsApp, тело `{"phone":"+77015556677"}`.
- `POST /api/v1/auth/otp/verify` — подтвердить WhatsApp-код; прежний ответ с `verification_token` для телефонной регистрации.
- `POST /api/v1/auth/email-otp/request` — отправить код на email, тело `{"email":"user@example.com"}`.
- `POST /api/v1/auth/email-otp/verify` — подтвердить email-код, тело `{"request_id":"UUID из отправки","code":"0123"}`.

Email-отправка возвращает `request_id`, `email`, `expires_in_seconds` и
`resend_after_seconds`. Проверка возвращает `request_id`, `email` и
`verified: true`. Этот ответ подтверждает email, автоматически не создаёт
аккаунт и не выдаёт JWT. Телефонная регистрация, вход и обновление токенов
сохраняют свой контракт; привязка email к аккаунту не выполняется автоматически.

Код состоит из четырёх цифр, хранится только в виде HMAC-хеша и действует
`OTP_EXPIRES_IN_SEC` секунд (по умолчанию 300). Email-коды не сохраняются в
отладочном хранилище кодов. После повторной отправки нужен последний код и
последний `request_id`. Использованный или истёкший код возвращает `400`.
Код одного канала нельзя подтвердить через маршрут другого канала.

Пауза между email-письмами — `OTP_RESEND_COOLDOWN`, по умолчанию 32 секунды;
лимит — `RATE_OTP_REQUEST_PER_DAY`, по умолчанию 10 попыток отправки на один
нормализованный адрес за последние 24 часа. Ограничения сохраняются в SQLite
и действуют также при одновременных запросах и после перезапуска сервера.
Неудачная отправка тоже учитывается; при отсутствии настройки Resend попытка
не списывается. Настройки WhatsApp не меняются.

После `OTP_MAX_ATTEMPTS` неверных кодов (по умолчанию 5) действует блокировка
`OTP_LOCK_DURATION_SEC` секунд (по умолчанию 120); затем нужно запросить новый
код. Ответ `429` содержит заголовок `Retry-After` и поле
`retry_after_seconds`: Flutter должен показать таймер и дождаться этого срока.
`502` означает ошибку отправки через Resend, `503` — недоступную конфигурацию
отправки. `200` при запросе означает приём письма провайдером, а не гарантию
попадания во входящие. Код и содержимое ошибок провайдера не возвращаются
клиенту. Русские примеры и пояснения доступны в Swagger в разделах
**OTP — Email** и **OTP — WhatsApp**.

## Notes API

- Auth create: `POST /api/v1/auth/notes`
- Calendar metadata: `GET /api/v1/auth/notes?from=2036-10-01&to=2036-10-31&fields=meta`
- Full date range: `GET /api/v1/auth/notes?from=2036-10-15&to=2036-10-15`
- Full selected day: `GET /api/v1/auth/notes/by-date?date=2036-10-15`
- One full note: `GET /api/v1/auth/notes/{id}`
- Legacy list: `GET /api/v1/auth/notes` (all notes, original response, no pagination)
- Auth update: `PUT /api/v1/auth/notes/{id}`
- Auth delete: `DELETE /api/v1/auth/notes/{id}`
- Legacy create: `POST /api/v1/notes`

Create and update note endpoints accept `multipart/form-data`:

- `note_type`: string
- `note_date`: optional `YYYY-MM-DD`, the day selected in the calendar
- `files`: repeatable file field, maximum `5` files

JSON requests are also supported for creating/updating notes without new files.
New clients should always supply `note_date` when creating a note. Omitting it
keeps old clients working: creation uses the current UTC day and returns
`note_date_source: "created_at"`. Explicit dates return `"selected"`. Updating a
note without `note_date` preserves its day and source. `createdAt` remains the
original server creation timestamp, even when a note moves to another day.

The range and day endpoints filter by `note_date`, not `createdAt`. Dates are
calendar strings with no time or timezone: Flutter must format the selected day
directly, without converting that day to UTC. A note created today for a future
day appears on that future day. To recover the selected day of an older note
from the app's local database, send `PUT /api/v1/auth/notes/{id}` with
`{"note_date":"2036-10-15"}`. The backend cannot recover a date it never received.

Range requests require both `from` and `to`, inclusive, with `from <= to`.
`page` defaults to `1`, `limit` to `100` (allowed `1..500`); the response includes
`data`, `total`, `page`, `limit`, `has_more`. `total` counts the whole matching
range. Invalid dates, duplicate known query parameters, invalid pagination,
unsupported `fields`, or `fields/page/limit` without a date range return `400`.
Results are ordered by `note_date`, then `createdAt`, then UUID, ascending.
An empty page is `data: []`; the day endpoint returns all notes of that day.
Concurrent changes between page requests can shift pages; repeat synchronization
if the collection changes while loading it.

`fields=meta` returns `NoteMetadata`: no `userId` or `note_files`. Full new
responses include `file_count`, `file_type`, `note_date`, `note_date_source` and
the existing fields. The unchanged legacy list deliberately keeps only the old
five note fields and its original `data`/`total` envelope.

File metadata is derived on creation and replacement. `file_type` describes the
first file by extension: `image`, `video`, `audio`, `document`; with no files it
is `""` and `file_count` is `0`. Files remain in their existing directories,
including `photo/` for images. On PUT, supplying files replaces all attachments;
omitting them preserves the attachments. Existing file download URLs and file
deletion behavior are preserved. Missing and other users' notes both return `404`.

Flutter integration:

1. Save `note_date` with each new note.
2. Load `fields=meta` for the calendar range, following pages until `has_more=false`.
3. Show badges using `note_date`; skip file downloads during this step.
4. When a day is selected, fetch `by-date`, then download only its needed files.
5. Use `GET /notes/{id}` for an individual refresh. All authenticated note routes
   require the user's Bearer access token; refresh it through the existing auth flow.

### OTP Testing Endpoint (DEV ONLY)

- Endpoint: `GET /api/v1/admin/testing/otp/latest`
- Requires:
  - `ENABLE_TESTING_ENDPOINTS=true`
- Query: provide `phone`

### Local OTP Flow

Request an OTP:

```bash
curl -X POST http://localhost:8081/api/v1/auth/otp/request \
  -H 'Content-Type: application/json' \
  -d '{"phone":"+77471850499"}'
```

Verify the delivered code:

```bash
curl -X POST http://localhost:8081/api/v1/auth/otp/verify \
  -H 'Content-Type: application/json' \
  -d '{"request_id":"<request_id>","code":"<code>"}'
```

### Permanent Access Token Endpoint (DEV ONLY)

- Endpoint: `POST /api/v1/admin/testing/auth/access-token`
- Requires:
  - `ENABLE_TESTING_ENDPOINTS=true`
- Body:
  - `phone`
- Behavior:
  - returns a user `access_token` without expiry for an existing user by phone number

## Tests

```bash
make test
```
