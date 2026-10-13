# Образ сайта — опция; основной вариант — бинарник под systemd (deploy/). Собирается целью make docker:
# бинарник строит make build (GOOS=linux) на машине с доступом к модулям Keelkit, образ его только копирует —
# SSH-ключ в сборку образа не попадает. TARGETARCH задаёт docker build --platform.
#
# Запуск: миграции — отдельной командой, как без Docker (serve откажется стартовать без них):
#   docker run --rm -v site-data:/data IMAGE migrate
#   docker run -d -p 8080:8080 -v site-data:/data -e BASE_URL=https://example.com IMAGE
# Бэкап: docker run --rm -v site-data:/data IMAGE backup (копии — в /data/backups того же тома).

# distroless static: сертификаты CA, tzdata, пользователь nonroot (65532), без shell. Digest закреплён.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

ARG TARGETARCH
COPY bin/linux-${TARGETARCH}/site /site

# Каталог тома: WORKDIR после USER создаёт его владельцем nonroot, и новый том наследует владельца.
USER nonroot:nonroot
WORKDIR /data
ENV ADDR=:8080 DATA_DIR=/data/data BACKUP_DIR=/data/backups
EXPOSE 8080
VOLUME /data

ENTRYPOINT ["/site"]
CMD ["serve"]
