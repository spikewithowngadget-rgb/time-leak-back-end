# Time Leak Backend

Go backend with SQLite, JWT auth, phone-only WhatsApp OTP auth (development stub), notes, and ads rotation.

## Features

- API versioning under `/api/v1`
- Existing user/profile/notes endpoints preserved
- Access token TTL is exactly `60s`
- Refresh token rotation with revocation
- OTP auth (phone-only WhatsApp):
  - `POST /api/v1/auth/otp/request`
  - `POST /api/v1/auth/otp/verify`
  - OTP stored hashed (HMAC-SHA256), 4-digit code, TTL `60s`
  - rate limit + attempts lockout
- Admin auth:
  - `POST /api/v1/admin/auth/login`
  - hardcoded defaults: `Admin` / `QRT123` (override with env)
- Ads engine:
  - Admin CRUD under `/api/v1/admin/ads`
  - Client rotation endpoint `GET /api/v1/ads/next`
- Swagger UI: `GET /swagger`

## Run

```bash
make run
```

Environment overrides:

- `APP_ADDR` (default `:8081`)
- `DB_PATH` (default `data`)
- `DB_NAME` (default `timeleak.db`)
- `JWT_ACCESS_SECRET`
- `JWT_ADMIN_SECRET`
- `JWT_REFRESH_TTL_HOURS`
- `OTP_HMAC_SECRET`
- `OTP_REQUEST_COOLDOWN_SEC`
- `OTP_MAX_ATTEMPTS`
- `OTP_LOCK_DURATION_SEC`
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

No manual migrate command is required.

## Swagger

- OpenAPI JSON: `GET /swagger.json`
- Swagger UI: `GET /swagger`

Swagger is static in `internal/handler/swagger.go`; regenerate by editing that file.

### OTP Testing Endpoint (DEV ONLY)

- Endpoint: `GET /api/v1/admin/testing/otp/latest`
- Requires:
  - `ENABLE_TESTING_ENDPOINTS=true`
  - Admin JWT in `Authorization: Bearer <token>`
- Query: provide `phone`

## Tests

```bash
make test
```
