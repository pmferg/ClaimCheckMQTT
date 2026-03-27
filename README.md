# ClaimCheck MQTT Gateway

A containerised Go service implementing the [Claim-Check pattern](https://www.enterpriseintegrationpatterns.com/patterns/messaging/StoreInLibrary.html) over MQTT with MinIO payload storage.

## How It Works

```
Producer                  Gateway                      Consumer
   │                        │                             │
   │  POST /ingest/{topic}  │                             │
   │  (raw payload body)    │                             │
   │───────────────────────>│                             │
   │                        │── store payload ──> MinIO   │
   │                        │── publish envelope ──> MQTT │
   │  202 { envelope }      │                             │
   │<───────────────────────│                             │
   │                        │     envelope on topic       │
   │                        │────────────────────────────>│
   │                        │                             │── detect claim-check
   │                        │                             │── GET payload from MinIO
   │                        │                             │── deliver full message
```

1. **Ingest** – A producer POSTs a payload to the gateway webhook at `/ingest/{topic}`.
2. **Store** – The gateway generates a unique MPID (Message Payload Identifier), stores the payload in MinIO.
3. **Publish** – A lightweight JSON envelope (containing the MPID, topic, content-type, size, timestamp) is published to the MQTT broker.
4. **Consume** – The subscriber client receives the envelope, detects the `claimcheck.v1` type, fetches the payload from MinIO via MPID, and delivers the full message to the application.

## Envelope Format

```json
{
  "version": "1.0",
  "type": "claimcheck.v1",
  "mpid": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
  "topic": "sensors/temperature",
  "source": "sensor-1",
  "content_type": "application/json",
  "payload_size": 128,
  "timestamp": 1711526400000
}
```

## Quick Start

```bash
docker compose up --build
```

This starts:
- **Mosquitto** MQTT broker on port 1883
- **MinIO** object storage on port 9000 (console on 9001)
- **Gateway** HTTP service on port 8080

### Send a message via the webhook

```bash
curl -X POST http://localhost:8080/ingest/sensors/temperature \
  -H "Content-Type: application/json" \
  -H "X-Source: demo" \
  -d '{"temperature": 22.5, "unit": "celsius"}'
```

### Retrieve a payload directly

```bash
curl http://localhost:8080/payload/{mpid}
```

## Go Client Libraries

### Publisher

```go
pub, _ := publisher.New(publisher.Config{
    BrokerURL: "tcp://localhost:1883",
    ClientID:  "my-publisher",
    QoS:       1,
}, store)

env, _ := pub.Send(ctx, "sensors/temp", "my-source", "application/json", payload)
```

### Subscriber

```go
sub, _ := subscriber.New(subscriber.Config{
    BrokerURL: "tcp://localhost:1883",
    ClientID:  "my-subscriber",
    QoS:       1,
}, store)

sub.Subscribe("sensors/#", func(msg subscriber.Message) {
    fmt.Printf("MPID=%s  payload=%s\n", msg.Envelope.MPID, msg.Payload)
})
```

The subscriber transparently resolves claim-check envelopes — your handler receives the full payload.

## Project Layout

```
cmd/gateway/          Gateway service entrypoint
pkg/claimcheck/       Core types: Envelope, MPID generation, detection
pkg/storage/          PayloadStore interface + MinIO implementation
pkg/gateway/          HTTP webhook handler
pkg/publisher/        Go client to publish claim-check messages
pkg/subscriber/       Go client to subscribe and auto-resolve payloads
examples/publish/     Example publisher program
examples/subscribe/   Example subscriber program
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | Gateway HTTP listen address |
| `MQTT_BROKER_URL` | `tcp://mqtt:1883` | MQTT broker URL |
| `MQTT_CLIENT_ID` | `claimcheck-gateway` | MQTT client ID for the gateway |
| `MINIO_ENDPOINT` | `minio:9000` | MinIO endpoint |
| `MINIO_ACCESS_KEY` | `minioadmin` | MinIO access key |
| `MINIO_SECRET_KEY` | `minioadmin` | MinIO secret key |
| `MINIO_BUCKET` | `claimcheck` | MinIO bucket name |
| `MINIO_USE_SSL` | `false` | Use TLS for MinIO |
