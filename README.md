# OLC RTC WebGUI

Самодостаточная веб-панель для установки, настройки и обновления [OLC RTC](https://github.com/openlibrecommunity/olcrtc) на VPS.

## Возможности

- вход по логину и паролю, защищённая HttpOnly-сессия и ограничение перебора;
- настройка провайдера, транспорта, домена, адреса, порта и upstream;
- генерация совместимого `.env` в каталоге OLC RTC;
- проверка версии через GitHub и обновление (`git pull --ff-only`);
- установка из GitHub и запуск через Docker Compose;
- запуск, остановка и перезапуск systemd-сервиса.

## Быстрый старт

```bash
go build -o olcrtcwebgui .
sudo install -m 0755 olcrtcwebgui /usr/local/bin/
sudo mkdir -p /var/lib/olcrtcwebgui
sudo env OLCRTC_WEB_ADMIN_PASSWORD='замените-на-длинный-пароль' \
  OLCRTC_WEB_ADDR='127.0.0.1:8080' \
  OLCRTC_WEB_DATA='/var/lib/olcrtcwebgui' \
  /usr/local/bin/olcrtcwebgui
```

Пароль должен содержать не меньше 12 символов. После первого запуска переменная пароля больше не требуется. Для замены пароля:

```bash
sudo env OLCRTC_WEB_DATA=/var/lib/olcrtcwebgui \
  OLCRTC_WEB_ADMIN_PASSWORD='новый-длинный-пароль' \
  olcrtcwebgui -set-password -username admin
```

Публикуйте панель только через HTTPS reverse proxy. При HTTPS установите `OLCRTC_WEB_SECURE_COOKIE=true`.

## Настройки окружения

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `OLCRTC_WEB_ADDR` | `127.0.0.1:8080` | Адрес панели |
| `OLCRTC_WEB_DATA` | `./data` | Закрытое хранилище настроек и учётных данных |
| `OLCRTC_DIR` | `/opt/olcrtc` | Каталог проекта OLC RTC |
| `OLCRTC_REPOSITORY` | официальный GitHub | Репозиторий обновлений |
| `OLCRTC_SERVICE` | `olcrtc` | Имя systemd-сервиса |
| `OLCRTC_WEB_SECURE_COOKIE` | `false` | Передавать cookie только через HTTPS |

Процессу нужны права на каталог OLC RTC, Docker и управление указанным systemd-сервисом. Рекомендуется выдать узкие разрешения через отдельного системного пользователя и `sudoers`, а не запускать панель от root.
