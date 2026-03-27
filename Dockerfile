FROM golang:1.25-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /publish  ./examples/publish \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /subscribe ./examples/subscribe

FROM alpine:3.21
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /publish  /usr/local/bin/publish
COPY --from=build /subscribe /usr/local/bin/subscribe

ENV MQTT_BROKER_URL=tcp://mqtt:1883 \
    MQTT_USERNAME=claimcheck \
    MQTT_PASSWORD=claimcheck123 \
    MINIO_ENDPOINT=minio:9000 \
    MINIO_ACCESS_KEY=minioadmin \
    MINIO_SECRET_KEY=minioadmin \
    MINIO_BUCKET=claimcheck
