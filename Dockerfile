FROM golang:1.25-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/olcrtcwebgui .

FROM alpine:3.22
RUN apk add --no-cache git docker-cli docker-cli-compose
COPY --from=build /out/olcrtcwebgui /usr/local/bin/olcrtcwebgui
ENV OLCRTC_WEB_ADDR=0.0.0.0:8080 OLCRTC_WEB_DATA=/data
VOLUME ["/data", "/opt/olcrtc"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/olcrtcwebgui"]
