FROM postgres:17-alpine
RUN apk add --no-cache restic
COPY infra/backup/backup.sh /usr/local/bin/mini-fury-backup
RUN chmod 755 /usr/local/bin/mini-fury-backup
ENTRYPOINT ["mini-fury-backup"]
