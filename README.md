# Time Leak Backend

Go backend with SQLite, JWT auth, phone-only WhatsApp OTP auth (development stub), notes, and ads rotation.

## Features

- API versioning under `/api/v1`
- Existing user/profile/notes endpoints preserved
- Notes support multipart uploads with up to `5` files per note
- Uploaded note files are stored under `./note_files/{audio|photo|video|document}`
- Access token TTL is exactly `60s`
- Refresh token rotation with revocation
- OTP auth (phone-only WhatsApp, demo stub):
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

Environment overrides:

- `APP_ADDR` (default `:8081`)
- `DB_PATH` (default `data`)
- `DB_NAME` (default `timeleak.db`)
- `NOTE_FILES_PATH` (default `./note_files`)
- `JWT_ACCESS_SECRET`
- `JWT_ADMIN_SECRET`
- `JWT_REFRESH_TTL_HOURS`
- `OTP_HMAC_SECRET`
- `OTP_REQUEST_COOLDOWN_SEC`
- `OTP_MAX_ATTEMPTS`
- `OTP_LOCK_DURATION_SEC`
- `OTP_EXPIRES_IN_SEC` (must be between `180` and `300`)
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

No manual migrate command is required.

## Swagger

- OpenAPI JSON: `GET /swagger.json`
- Swagger UI: `GET /swagger`

Swagger is static in `internal/handler/swagger.go`; regenerate by editing that file.

## Notes API

- Auth create: `POST /api/v1/auth/notes`
- Auth list: `GET /api/v1/auth/notes`
- Auth update: `PUT /api/v1/auth/notes/{id}`
- Auth delete: `DELETE /api/v1/auth/notes/{id}`
- Legacy create: `POST /api/v1/notes`

Create and update note endpoints accept `multipart/form-data`:

- `note_type`: string
- `files`: repeatable file field, maximum `5` files

Note responses include `note_files` as downloadable URLs for the mobile app.

### OTP Testing Endpoint (DEV ONLY)

- Endpoint: `GET /api/v1/admin/testing/otp/latest`
- Requires:
  - `ENABLE_TESTING_ENDPOINTS=true`
- Query: provide `phone`

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
