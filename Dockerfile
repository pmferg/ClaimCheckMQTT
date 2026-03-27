FROM golang:1.25-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /gateway ./cmd/gateway

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /gateway /gateway

ENV LISTEN_ADDR=:8080 \
    MQTT_BROKER_URL=tcp://mqtt:1883 \
    MQTT_CLIENT_ID=claimcheck-gateway \
    MQTT_USERNAME=claimcheck \
    MQTT_PASSWORD=claimcheck123 \
    MINIO_ENDPOINT=minio:9000 \
    MINIO_ACCESS_KEY=minioadmin \
    MINIO_SECRET_KEY=minioadmin \
    MINIO_BUCKET=claimcheck \
    MINIO_USE_SSL=false

EXPOSE 8080
ENTRYPOINT ["/gateway"]
