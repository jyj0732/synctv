From alpine:3 as builder

ARG VERSION=v0.0.0

WORKDIR /synctv

COPY ./ ./

RUN apk add --no-cache bash curl gcc git go musl-dev

RUN bash build.sh -P -v ${VERSION} -b build

From alpine:3

WORKDIR /opt/synctv

ENV SERVER_LISTEN=0.0.0.0

ENV SERVER_PORT=8080

COPY --from=builder /synctv/build/synctv /usr/local/bin/synctv

COPY entrypoint.sh /entrypoint.sh

RUN apk add --no-cache bash ca-certificates su-exec tzdata

RUN chmod +x /entrypoint.sh

ENV PUID=0 PGID=0 UMASK=022

EXPOSE 8080 8080

CMD [ "/entrypoint.sh" ]