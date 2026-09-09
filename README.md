# OLC RTC WebGUI

Самодостаточная веб-панель для установки, настройки и обновления [OLC RTC](https://github.com/openlibrecommunity/olcrtc) на VPS.

## Возможности

- адаптивный тёмный интерфейс управления в техно-стилистике OLC RTC;
- вход по логину и паролю, защищённая HttpOnly-сессия и ограничение перебора;
- выбор режима `srv` или `cnc`, провайдера (`jitsi`, `telemost`, `wbstream`, `none`) и транспорта (`datachannel`, `vp8channel`, `seichannel`, `videochannel`);
- настройки комнаты, шифрования, DNS, SOCKS5, engine, liveness, lifecycle, traffic и параметров видео-транспортов;
- несколько именованных профилей провайдеров в одном `olcrtc.yaml` с настройками failover и изменяемым порядком;
- генерация клиентских URI `olcrtc://`, копирование ссылки и локальный QR-код для каждого профиля;
- подсказки по матрице совместимости provider + transport и генерация 32-байтного ключа;
- генерация строгого `olcrtc.yaml`, который принимает актуальный CLI OLC RTC;
- проверка версии через GitHub и fast-forward обновление до upstream HEAD;
- установка или fast-forward обновление исходников, сборка через Mage и автоматическая установка systemd-сервиса;
- отображение RAM/SWAP и автоматическое включение swap-файла на 4 ГБ перед сборкой на малом VPS;
- встроенная диагностика VPS: uptime, load average, память, SWAP и последние строки журналов OLC RTC/WebGUI;
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
2. если RAM меньше 4 ГБ и SWAP отсутствует, создаёт swap-файл на 4 ГБ, устанавливает права `0600` и включает его;
3. запускает `mage -d /opt/olcrtc build` (если Mage не установлен — `go run github.com/magefile/mage@latest`);
4. создаёт `/etc/systemd/system/olcrtc.service` с путями к бинарнику и YAML-конфигу;
5. выполняет `systemctl daemon-reload` и `systemctl enable olcrtc.service`.

Сервис не запускается автоматически: после успешной установки проверьте конфигурацию и нажмите **«Запустить»**. Создаваемый unit эквивалентен следующему:

```ini
[Service]
ExecStart=/opt/olcrtc/build/olcrtc-linux-amd64 /opt/olcrtc/olcrtc.yaml
Restart=on-failure
```

Имя бинарника соответствует формату Mage `olcrtc-<GOOS>-<GOARCH>`; например, на ARM64 панель использует `olcrtc-linux-arm64`.

По умолчанию панель сохраняет конфиг в `/opt/olcrtc/olcrtc.yaml`; путь можно изменить переменной `OLCRTC_CONFIG`.
Пути `OLCRTC_DIR` и `OLCRTC_CONFIG` должны быть абсолютными и не содержать пробелов: перед установкой панель проверяет их и запускает `systemd-analyze verify` для созданного unit-файла.

## Настройки окружения

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `OLCRTC_WEB_ADDR` | `127.0.0.1:8080` | Адрес панели |
| `OLCRTC_WEB_DATA` | `./data` | Закрытое хранилище настроек и учётных данных |
| `OLCRTC_DIR` | `/opt/olcrtc` | Каталог проекта OLC RTC |
| `OLCRTC_CONFIG` | `$OLCRTC_DIR/olcrtc.yaml` | Путь к создаваемому YAML-конфигу |
| `OLCRTC_REPOSITORY` | официальный GitHub | Репозиторий обновлений |
| `OLCRTC_SERVICE` | `olcrtc` | Имя systemd-сервиса |
| `OLCRTC_WEB_SERVICE` | `olcrtcwebgui` | Имя systemd-сервиса самой веб-панели для просмотра журнала |
| `OLCRTC_SYSTEMD_DIR` | `/etc/systemd/system` | Каталог для создаваемого unit-файла |
| `OLCRTC_GO_CACHE` | `$OLCRTC_WEB_DATA/go-cache` | Каталог GOPATH, модулей и сборочного кэша Go |
| `OLCRTC_SWAP_FILE` | `/swapfile` | Swap-файл, автоматически включаемый при RAM меньше 4 ГБ и отсутствии SWAP |
| `OLCRTC_WEB_SECURE_COOKIE` | `false` | Передавать cookie только через HTTPS |

На сервере должны быть установлены `git`, Go версии из `go.mod` OLC RTC (на момент написания — 1.26 или новее) и systemd. Процессу панели нужны права на каталог OLC RTC, запись unit-файла в `OLCRTC_SYSTEMD_DIR`, чтение журналов через `journalctl -u` и выполнение `systemctl daemon-reload`, `enable`, `start`, `stop`, `restart` и `is-active` для указанного сервиса. Проще всего проверить установку первым запуском панели от root; для постоянной эксплуатации лучше выдать отдельному системному пользователю минимальные ACL/polkit-разрешения.

Пункт **«Диагностика»** в боковом меню показывает состояние Linux-хоста и последние 200 строк двух журналов: `OLCRTC_SERVICE` и `OLCRTC_WEB_SERVICE`. Общесистемный журнал не читается. Endpoint диагностики защищён той же авторизацией, что и остальные функции панели.

Панель всегда задаёт `GOPATH`, `GOMODCACHE` и `GOCACHE` внутри `OLCRTC_GO_CACHE`, поэтому сборка работает и в systemd-сервисе без переменной `HOME`. Каталог должен быть доступен процессу панели для записи.

## Профили и подключение клиента

На основном экране конфигурации отображается компактный список профилей, например `jitsi-main` и `wb-backup`. Кнопка **«Конфигурация»** открывает отдельное окно настроек выбранного профиля. Панель записывает профили в нативный массив `profiles[]`; OLC RTC перебирает их по порядку с параметрами `failover.retry_delay` и `failover.max_cycles`. Порядок и параметры профилей на серверной и клиентской стороне должны совпадать.

Кнопка **«Подключение клиента»** сразу генерирует URI формата `olcrtc://<provider>?<transport>...@<room>#<key>$<comment>` и QR-код, а затем открывает их в отдельном окне. QR формируется внутри WebGUI и не отправляется внешним сервисам. URI содержит ключ шифрования, поэтому обращайтесь со ссылкой и QR-кодом как с секретом.

Полный цикл установки и управление сервисом рассчитаны на запуск панели непосредственно на Linux-хосте с systemd. Контейнер Docker не может управлять systemd хоста без небезопасного проброса системных сокетов и каталогов, поэтому для этого режима Docker-образ не используется.

## Шрифты

Интерфейс использует локально встроенный **Exo 2** для заголовков, меню и кнопок и **JetBrains Mono** для путей, версий, показателей и журналов. Панель не обращается к Google Fonts или другим внешним CDN. Оба шрифта распространяются по SIL Open Font License 1.1; тексты лицензий находятся в `web/fonts/`.

