# Systemd Service Setup для Time Leak Backend

Этот файл содержит инструкции для настройки автоматического запуска проекта через systemctl.

## Файлы

- **time-leak.service** - конфигурационный файл systemd сервиса
- **install-service.sh** - скрипт установки сервиса

## Установка

### Шаг 1: Убедитесь, что Go установлен

```bash
go version
```

Если Go не установлен, скачайте с https://golang.org/dl/

### Шаг 2: Запустите скрипт установки с правами root

```bash
chmod +x install-service.sh
sudo ./install-service.sh
```

Скрипт автоматически:
- Скомпилирует Go приложение
- Скопирует сервис-файл в `/etc/systemd/system/`
- Включит автозапуск при загрузке системы
- Запустит сервис

### Шаг 3: Проверьте статус сервиса

```bash
sudo systemctl status time-leak
```

## Использование

### Запустить сервис
```bash
sudo systemctl start time-leak
```

### Остановить сервис
```bash
sudo systemctl stop time-leak
```

### Перезагрузить сервис
```bash
sudo systemctl restart time-leak
```

### Просмотреть логи
```bash
sudo journalctl -u time-leak -f
```

Или последние 50 строк логов:
```bash
sudo journalctl -u time-leak -n 50
```

### Включить/отключить автозапуск
```bash
# Включить автозапуск
sudo systemctl enable time-leak

# Отключить автозапуск
sudo systemctl disable time-leak
```

## Production-сервис `server.service`

В production используется юнит `server.service`; установочный шаблон этого
репозитория создаёт `time-leak.service`. Для работающего production-процесса
используйте его фактическое имя:

```bash
sudo systemctl status server.service --no-pager
sudo journalctl -u server.service --since '1 hour ago' --no-pager --grep='password reset'
sudo journalctl -u server.service -f
```

Сброс пароля пишет `password reset rejected` с причиной в поле `reason`
(например, `verification_token_expired`), `password reset failed` при внутренней
ошибке и `password reset completed` при успехе. Пароль и verification token
в эти диагностические записи не включаются.

`git push` сам по себе не обновляет запущенный процесс. После получения нового
кода на сервере проверьте способ запуска и рабочий каталог:

```bash
sudo systemctl show server.service -p WorkingDirectory -p ExecStart
```

Если `ExecStart` запускает бинарный файл, пересоберите и установите его по
указанному пути; при `go run` новый код компилируется при запуске. Затем
перезапустите сервис и проверьте его статус:

```bash
sudo systemctl restart server.service
sudo systemctl status server.service --no-pager
```

`daemon-reload` нужен при изменении самого unit-файла или drop-in, а не при
обычном обновлении кода. Новые ответы reset confirm с полями `error` и `code`
и диагностические записи появятся после запуска обновлённой версии.

## Конфигурация

Параметры сервиса в файле `time-leak.service`:

- **APP_ADDR** - адрес слушания (по умолчанию `:8080`)
- **DB_PATH** - путь к директории базы данных (по умолчанию `/home/time-leak-back-end/data`)
- **DB_NAME** - имя файла БД (по умолчанию `timeleak.db`)
- **WHAPI_BASE_URL** - базовый URL WhatsApp-интегратора (по умолчанию `https://whapi.kz`)
- **WHAPI_TOKEN** - секретный токен WHAPI, задавайте через переменные окружения или отдельный защищенный EnvironmentFile
- **WHAPI_TIMEOUT** - таймаут запроса к WHAPI (по умолчанию `10s`)
- **ENABLE_TESTING_ENDPOINTS** - включает DEV endpoint `/api/v1/admin/testing/otp/latest` (ставьте `true` только для dev/staging)
- **User** - пользователь, от которого запускается сервис (по умолчанию `root`)

Для изменения параметров отредактируйте `/etc/systemd/system/time-leak.service` и перезагрузите сервис:

```bash
sudo systemctl daemon-reload
sudo systemctl restart time-leak
```

## Удаление сервиса

```bash
sudo systemctl stop time-leak
sudo systemctl disable time-leak
sudo rm /etc/systemd/system/time-leak.service
sudo systemctl daemon-reload
```

## Особенности

- ✅ Автоматический перезапуск при сбое
- ✅ Логирование через journalctl
- ✅ Автозапуск при загрузке системы
- ✅ Graceful shutdown (обработка SIGTERM)
- ✅ Ограничение открытых файлов (65535)
