# Конфигурация, резервные копии, развёртывание

Часть `AGENTS.md` сайта: читается, когда задача касается этой темы.

## Конфигурация (переменные окружения)

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `ADDR` | `:8080` (в `make run`/`dev` — `:9090`) | адрес HTTP-сервера |
| `DATA_DIR` | `data` | каталог с БД (`site.db`) и загрузками (`uploads/`: оригиналы и варианты изображений; `files/`: файлы модуля `files`) |
| `BACKUP_DIR` | `backups` | каталог снимков БД перед миграциями и резервных копий `site backup` |
| `COOKIE_SECURE` | `true` | флаг `Secure` у cookie админки и согласия с баннером cookies; `false` — только для разработки по http (`make run`/`make dev` передают `false`) |
| `TZ` | часовой пояс системы | в нём показываются и вводятся даты публикации постов (в БД — UTC) |
| `BASE_URL` | — (в `make run`/`dev` — `http://localhost:<порт из ADDR>`) | адрес сайта (`https://example.com`, без пути) для canonical, Open Graph, sitemap, RSS и robots.txt. **Обязателен для `serve`**: без него сайт не стартует (адрес из заголовка `Host` подделывается). Слеш в конце отбрасывается |

## Резервные копии

- `site backup` (`make backup`) пишет в `BACKUP_DIR` пару с одной меткой времени UTC: `<время>.db` — снимок БД
  (`VACUUM INTO`, как перед миграциями) и `<время>.tar.gz` — `uploads/` и `files/` из `DATA_DIR` с этими путями
  в архиве. Права обоих — `0600` (в БД хеши паролей и сессии). Архив появляется только целиком (временный файл
  `.backup-*.tmp` и rename); недописанные файлы хранилища (`*.tmp-*`) в него не попадают.
- Сервер может работать. Сначала снимок, потом архив: хранилище пишет файл до строки в БД и удаляет после, поэтому
  всё, на что ссылается снимок, уже на диске. Пропасть может только файл, удалённый во время архивации (редактор удалил
  изображение, идёт `reprocess-images`), — он перечисляется в логе (`file deleted during backup`).
  Если `DATA_DIR/site.db` нет — ошибка, а не снимок пустой БД.
- На сервере — таймер systemd `deploy/site-backup.timer` (каждую ночь в 03:30) запускает `deploy/site-backup.service`:
  `site backup`, затем `find` удаляет копии старше 14 дней (и снимки перед миграциями). Вручную —
  `systemctl start site-backup`, журнал — `journalctl -u site-backup`. Без systemd — то же строкой cron от пользователя сайта:
  `30 3 * * * site set -a; . /etc/site/site.env; /opt/site/site backup && find "$BACKUP_DIR" -name '*Z.*' -mtime +14 -delete`.
  Копии на том же диске не спасают от потери диска или сервера: их копирование в другое место (rsync, rclone,
  снимки диска у хостера) настраивается для каждого сайта. При хранилище S3 загрузки защищает версионирование бакета.
- Восстановление — в пустой `DATA_DIR` (не поверх: старые `site.db-wal` и `-shm` испортят снимок):
  ```sh
  cd /var/lib/site && systemctl stop site && mv data data.old && install -d -o site -g site -m 0750 data
  tar -xzf backups/<время>.tar.gz -C data && cp backups/<время>.db data/site.db && chown -R site:site data
  systemctl start site-migrate && systemctl start site     # site-migrate — если бинарник новее копии
  ```
  Тест `TestBackupRestore` (`cmd/site/backup_test.go`) проверяет этот путь: сайт из копии отдаёт изображения и файлы.

## Развёртывание

Основной вариант — бинарник под systemd на VPS (`deploy/`); образ Docker — опция.

| Файл | Куда | Что |
|---|---|---|
| `bin/site` (`make build GOOS=linux GOARCH=amd64`) | `/opt/site/site` | бинарник |
| `deploy/site.env.example` | `/etc/site/site.env` (`root:site`, `0640`) | настройки: `ADDR=127.0.0.1:8080`, `BASE_URL`, `DATA_DIR`, `BACKUP_DIR`, `TZ` |
| `deploy/site.service` | `/etc/systemd/system/` | `site serve`; `Restart=on-failure`, ограничения systemd (`systemd-analyze security` — 1.7) |
| `deploy/site-migrate.service` | `/etc/systemd/system/` | `site migrate`, только вручную — не `ExecStartPre`: миграции не применяются при старте |
| `deploy/site-backup.service`, `.timer` | `/etc/systemd/system/` | ночной `site backup` (см. «Резервные копии») |

- Данные — `/var/lib/site` (`StateDirectory`: создаёт systemd, владелец — пользователь `site`, `0750`); `DATA_DIR` и
  `BACKUP_DIR` — внутри. Всё остальное для процесса только чтение (`ProtectSystem=strict`).
- Первая установка:
  ```sh
  useradd -r -s /usr/sbin/nologin site && install -d /opt/site /etc/site
  install -m 0755 site /opt/site/site
  install -o root -g site -m 0640 site.env /etc/site/site.env      # из deploy/site.env.example
  cp site.service site-migrate.service site-backup.service site-backup.timer /etc/systemd/system/
  systemctl daemon-reload && systemctl start site-migrate
  systemctl enable --now site site-backup.timer
  sudo -u site sh -c 'set -a; . /etc/site/site.env; /opt/site/site user create me@example.com'
  ```
- Обновление: новый бинарник рядом, затем остановка, миграции (снимок БД — внутри `site migrate`), замена, старт:
  ```sh
  install -m 0755 site /opt/site/site.new
  systemctl stop site && mv /opt/site/site.new /opt/site/site && systemctl start site-migrate && systemctl start site
  curl -s 127.0.0.1:8080/healthz      # version — новая, pending — 0
  ```
  С непримененными миграциями `serve` не стартует — systemd перезапускает его и пишет причину в `journalctl -u site`.
- Сайт слушает только localhost; TLS и порты 80/443 — у reverse proxy. Caddy сам получает сертификат:
  `example.com { reverse_proxy 127.0.0.1:8080 }`. Без https вход в админку не работает: cookie с флагом `Secure`
  (`COOKIE_SECURE` по умолчанию `true`). Ограничение попыток входа (модуль `auth`) считается по email, поэтому прокси
  ему не мешает; в логе входа `remote_addr` — адрес прокси.
- Несколько сайтов на одном сервере — свои пользователь, юниты, `StateDirectory`, `/etc/<имя>/` и порт `ADDR`.

**Docker (опция).** `make docker` собирает бинарник под Linux (`bin/linux-<arch>/site`) и образ `DOCKER_IMAGE:VERSION`
(`Dockerfile`: distroless `static-debian13:nonroot` с закреплённым digest, пользователь 65532, без shell). Бинарник
строится на машине разработки, а не в образе; `.dockerignore` пропускает
в контекст только бинарники. Для VPS на amd64 — `make docker DOCKER_ARCH=amd64`. Данные и копии — в томе `/data`:
```sh
docker run --rm -v site-data:/data keelkit-reference migrate
docker run -d --name site -p 127.0.0.1:8080:8080 -v site-data:/data -e BASE_URL=https://example.com keelkit-reference
docker exec site /site backup                            # копии — в /data/backups того же тома
```

**litestream (опция).** Непрерывная репликация БД в S3-совместимое хранилище: WAL уходит каждую секунду, после
потери сервера теряются секунды, а не сутки. Сайт уже в режиме WAL, код не меняется: litestream — отдельный процесс
(свой systemd-юнит из его пакета) рядом с `site.service`. Загрузки (`uploads/`, `files/`) он не копирует — их
по-прежнему защищает `site backup` и копирование архивов на другой сервер. Конфиг `/etc/litestream.yml`:
```yaml
dbs:
  - path: /var/lib/site/data/site.db
    replicas:
      - url: s3://<бакет>/site.db     # ключи — LITESTREAM_ACCESS_KEY_ID / LITESTREAM_SECRET_ACCESS_KEY
```
Восстановление: `litestream restore -o /var/lib/site/data/site.db s3://<бакет>/site.db` в пустой `DATA_DIR`, затем
архив загрузок. Версия — последний стабильный релиз (`v0.5.17` на 2026-10-14), проверки — по разделу «Зависимости»
(`docs/dependencies.md` монорепо); решается для каждого сайта.
