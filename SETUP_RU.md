# Версия 2

Актуальные инструкции и новые функции: [UPDATE_V7_RU.md](UPDATE_V7_RU.md).

# Запуск и настройка

## Быстрый запуск в Windows

После сборки из корня проекта один раз запустите `.\setup-forum.ps1`. Выберите срок сессии в часах, затем введите только нужные интеграции: Google/GitHub можно оставить пустыми, SMTP тоже можно пропустить для локальной разработки. Пароли вводятся скрыто и сохраняются зашифрованными для вашей учётной записи Windows в `.local/forum-secrets.xml`. Для изменения срока в отдельном запуске задайте, например, `$env:SESSION_TTL = "12h"`. Затем запустите `.\start-forum.ps1`.

Перед этим соберите приложение:

```powershell
$env:CGO_ENABLED = "1"
go build -o forum-v7.exe ./cmd
```

Для каждой новой самостоятельной установки владелец создаёт свои OAuth-приложения и вводит свои ключи. Посетители готового форума OAuth-ключи не вводят.

## Windows

Нужны Go 1.23.4 или новее и 64-битный GCC в PATH: SQLite-драйвер требует CGO.

В PowerShell из папки проекта:

```powershell
$env:CGO_ENABLED = "1"
go test ./...
go build -o forum-v7.exe ./cmd
if ($LASTEXITCODE -eq 0) { .\forum-v7.exe }
```

По умолчанию: https://localhost:4000. Остановите старый экземпляр форума перед запуском нового на том же порту. Данные остаются в `forum.db`, изображения — в `uploads`. Новая таблица `oauth_accounts` создаётся автоматически, существующие пользователи и посты не удаляются. Старые сессии в памяти не переносятся: после обновления нужно войти снова.

Локальный сертификат теперь индивидуален и генерируется в `.local/tls`; общего встроенного ключа больше нет. Браузер может предупредить о новом сертификате. Для локальной работы без предупреждения о сертификате можно выбрать HTTP на loopback:

```powershell
$env:FORUM_HTTP = "1"
$env:PUBLIC_BASE_URL = "http://localhost:4000"
.\forum-v7.exe
```

Не смешивайте `localhost` и `127.0.0.1`: адрес в браузере, PUBLIC_BASE_URL и callback у провайдера должны совпадать.

## Google

1. В Google Cloud создайте проект, настройте Google Auth Platform / consent screen: Branding, Audience, Data Access. В режиме Testing добавьте свой аккаунт в Test users.
2. Создайте OAuth client типа **Web application**.
3. Добавьте Authorized redirect URI: `https://localhost:4000/google/callback`.
4. Задайте переменные перед запуском:

```powershell
$env:PUBLIC_BASE_URL = "https://localhost:4000"
$env:GOOGLE_CLIENT_ID = "ваш-client-id"
$env:GOOGLE_CLIENT_SECRET = "ваш-client-secret"
```

Если выбрали локальный HTTP, зарегистрируйте `http://localhost:4000/google/callback` и установите PUBLIC_BASE_URL с `http`.

Документация: https://developers.google.com/identity/protocols/oauth2/web-server

## GitHub

1. Откройте GitHub → Settings → Developer settings → OAuth Apps → New OAuth App.
2. Homepage URL: `https://localhost:4000`.
3. Authorization callback URL: `https://localhost:4000/github/callback`.
4. Сгенерируйте Client secret и задайте:

```powershell
$env:GITHUB_CLIENT_ID = "ваш-client-id"
$env:GITHUB_CLIENT_SECRET = "ваш-client-secret"
.\forum-v7.exe
```

Для HTTP замените схему в обоих адресах. На GitHub нужна подтверждённая почта; скрывать её в публичном профиле можно. Приложение запрашивает `user:email` и читает подтверждённую почту через `/user/emails`.

Документация: https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps

Без ключей кнопка соответствующего провайдера показана как недоступная. После задания переменных перезапустите сервер; пересобирать его не нужно. Ключи не отправляйте в чат и не добавляйте в Git.

Существующие локальные и социальные аккаунты с одинаковой почтой автоматически не объединяются. Войдите исходным способом, откройте My profile и выберите Connect Google / Connect GitHub. Для новых OAuth-аккаунтов локальный пароль не создаётся; используйте кнопку своего провайдера.

## Переменные окружения

| Переменная | Значение по умолчанию / назначение |
|---|---|
| `PUBLIC_BASE_URL` | `https://localhost:4000`, без завершающего пути |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | Google OAuth |
| `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | GitHub OAuth |
| `FORUM_ADDR` | `127.0.0.1:4000`, адрес прослушивания |
| `FORUM_DB_PATH` | `./forum.db`; отдельный путь для тестирования |
| `FORUM_HTTP` | `1` включает HTTP для локальной разработки |
| `SESSION_TTL` | `24h` по умолчанию; от `5m` до `720h` |
| `TLS_CERT_FILE`, `TLS_KEY_FILE` | Пара путей к собственному TLS-сертификату и ключу |

Приложение читает переменные окружения процесса; файлы `.env` автоматически не загружаются. Переменные `$env:...` действуют в текущем PowerShell и запускаемых из него процессах.

Для публикации используйте свой сертификат и отдельные OAuth-приложения/адреса. Если используете reverse proxy, текущая проверка Origin требует корректного публичного Host и HTTPS на соединении с Go. Не доверяйте произвольным заголовкам X-Forwarded-* от клиента. Доверенные прокси задаются через TRUSTED_PROXY_CIDRS, остальные заголовки пересылки игнорируются.

Для контейнерного запуска подготовлены многоэтапный Dockerfile, compose.yaml и Caddyfile; подробности в UPDATE_V7_RU.md.

## Что проверено

`go test ./...`: формы, экранирование ввода, дубликаты, смена и истечение сессий, OAuth с подставными HTTP-ответами Google/GitHub, приватная почта GitHub, проверка state/PKCE, коллизии аккаунтов, серверные права и Origin. Тесты используют отдельные временные БД.

Проверка браузером использовала отдельную `preview.db`, а не вашу `forum.db`. Проверены сохранение формы при слабом пароле, вход, создание поста, страницы управления, широкая и мобильная вёрстка. Настоящий OAuth-вход ещё требует ваших ключей и проверки в браузере с аккаунтом провайдера.
