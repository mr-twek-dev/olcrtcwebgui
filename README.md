# OLC RTC WebGUI

Самодостаточная веб-панель для установки, настройки и обновления [OLC RTC](https://github.com/openlibrecommunity/olcrtc) на VPS.

## Возможности

- вход по логину и паролю, защищённая HttpOnly-сессия и ограничение перебора;
- выбор режима `srv` или `cnc`, провайдера (`jitsi`, `telemost`, `wbstream`, `none`) и транспорта (`datachannel`, `vp8channel`, `seichannel`, `videochannel`);
- настройки комнаты, шифрования, DNS, SOCKS5, engine, liveness, lifecycle, traffic и параметров видео-транспортов;
- подсказки по матрице совместимости provider + transport и генерация 32-байтного ключа;
- генерация строгого `olcrtc.yaml`, который принимает актуальный CLI OLC RTC;
- проверка версии через GitHub и fast-forward обновление до upstream HEAD;
- установка или fast-forward обновление исходников, сборка через Mage и автоматическая установка systemd-сервиса;
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

Кнопка **«Установить / обновить»** выполняет весь цикл установки:

1. загружает или обновляет исходники OLC RTC;
2. запускает `mage -d /opt/olcrtc build` (если Mage не установлен — `go run github.com/magefile/mage@latest`);
3. создаёт `/etc/systemd/system/olcrtc.service` с путями к бинарнику и YAML-конфигу;
4. выполняет `systemctl daemon-reload` и `systemctl enable olcrtc.service`.

Сервис не запускается автоматически: после успешной установки проверьте конфигурацию и нажмите **«Запустить»**. Создаваемый unit эквивалентен следующему:

```ini
[Service]
ExecStart=/opt/olcrtc/build/olcrtc /opt/olcrtc/olcrtc.yaml
Restart=on-failure
```

По умолчанию панель сохраняет конфиг в `/opt/olcrtc/olcrtc.yaml`; путь можно изменить переменной `OLCRTC_CONFIG`.

## Настройки окружения

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `OLCRTC_WEB_ADDR` | `127.0.0.1:8080` | Адрес панели |
| `OLCRTC_WEB_DATA` | `./data` | Закрытое хранилище настроек и учётных данных |
| `OLCRTC_DIR` | `/opt/olcrtc` | Каталог проекта OLC RTC |
| `OLCRTC_CONFIG` | `$OLCRTC_DIR/olcrtc.yaml` | Путь к создаваемому YAML-конфигу |
| `OLCRTC_REPOSITORY` | официальный GitHub | Репозиторий обновлений |
| `OLCRTC_SERVICE` | `olcrtc` | Имя systemd-сервиса |
| `OLCRTC_SYSTEMD_DIR` | `/etc/systemd/system` | Каталог для создаваемого unit-файла |
| `OLCRTC_WEB_SECURE_COOKIE` | `false` | Передавать cookie только через HTTPS |

На сервере должны быть установлены `git`, Go версии из `go.mod` OLC RTC (на момент написания — 1.26 или новее) и systemd. Процессу панели нужны права на каталог OLC RTC, запись unit-файла в `OLCRTC_SYSTEMD_DIR` и выполнение `systemctl daemon-reload`, `enable`, `start`, `stop`, `restart` и `is-active` для указанного сервиса. Проще всего проверить установку первым запуском панели от root; для постоянной эксплуатации лучше выдать отдельному системному пользователю минимальные ACL/polkit-разрешения.

Полный цикл установки и управление сервисом рассчитаны на запуск панели непосредственно на Linux-хосте с systemd. Контейнер Docker не может управлять systemd хоста без небезопасного проброса системных сокетов и каталогов, поэтому для этого режима Docker-образ не используется.

