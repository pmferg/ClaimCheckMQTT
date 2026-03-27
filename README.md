# ClaimCheck MQTT

Go client libraries implementing the [Claim-Check pattern](https://www.enterpriseintegrationpatterns.com/patterns/messaging/StoreInLibrary.html) over MQTT with MinIO payload storage and built-in callback support.

## How It Works

```
Publisher                                            Subscriber
   │                                                      │
   │── store payload ──> MinIO                            │
   │── publish envelope ──> MQTT ────────────────────────>│
   │                                          detect claim-check
   │                                          fetch payload from MinIO
   │                                                      │
   │              (if callback topic set)                  │
   │                                                      │
   │<──── callback envelope on callback topic ────────────│
   │      (response payload also in MinIO)    store resp ─┘
   │                                                      │
   │<──── callback close signal ──────────────────────────│
   │      (publisher closes channel)                      │
```

1. **Publish** – The publisher stores the payload in MinIO, then publishes a lightweight JSON envelope to the MQTT topic.
2. **Subscribe** – The subscriber receives the envelope, detects the `claimcheck.v1` type, fetches the payload from MinIO via MPID, and delivers the full message to the application.
3. **Callback** – If the publisher used `SendWithCallback`, the envelope includes a unique callback topic. The subscriber can `Reply()` (claim-check wrapped) any number of times, then `CloseCallback()` to signal completion. The publisher receives responses on a Go channel that closes automatically.

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
  "timestamp": 1711526400000,
  "callback_topic": "claimcheck/callbacks/f7e8d9c0-..."
}
```

The `callback_topic` field is only present when the publisher expects responses.

A callback close signal uses type `claimcheck.callback.close.v1` with no MPID or payload.

## Quick Start

Start the infrastructure:

```bash
docker compose up -d
```

This starts:
- **Mosquitto** MQTT broker on port 1883 (authenticated)
- **MinIO** object storage on port 9000 (console on 9001)

### Run the subscriber

```bash
go run ./examples/subscribe
```

### Run the publisher (with callback)

```bash
go run ./examples/publish
```

The publisher sends a message with a callback topic, the subscriber auto-replies with an acknowledgement and closes the callback, and the publisher prints the response then exits.

## Go Client Libraries

### Publisher

```go
store, _ := storage.NewMinIO(ctx, storage.MinIOConfig{...})

pub, _ := publisher.New(publisher.Config{
    BrokerURL: "tcp://localhost:1883",
    ClientID:  "my-publisher",
    Username:  "claimcheck",
    Password:  "claimcheck123",
    QoS:       1,
}, store)

// Fire-and-forget
env, _ := pub.Send(ctx, "sensors/temp", "my-source", "application/json", payload)

// With callback channel
env, ch, _ := pub.SendWithCallback(ctx, "sensors/temp", "my-source", "application/json", payload)
for msg := range ch {
    fmt.Printf("response: %s\n", msg.Payload)
}
```

### Subscriber

```go
store, _ := storage.NewMinIO(ctx, storage.MinIOConfig{...})

sub, _ := subscriber.New(subscriber.Config{
    BrokerURL: "tcp://localhost:1883",
    ClientID:  "my-subscriber",
    Username:  "claimcheck",
    Password:  "claimcheck123",
    QoS:       1,
}, store)

sub.Subscribe("sensors/#", func(msg subscriber.Message) {
    fmt.Printf("payload: %s\n", msg.Payload)

    if msg.HasCallback() {
        msg.Reply(ctx, "application/json", []byte(`{"ack":true}`))
        msg.CloseCallback()
    }
})
```

## Project Layout

```
pkg/claimcheck/       Core types: Envelope, MPID generation, detection
pkg/storage/          PayloadStore interface + MinIO implementation
pkg/publisher/        Go client to publish claim-check messages (with callback)
pkg/subscriber/       Go client to subscribe, auto-resolve, and reply
examples/publish/     Example publisher with callback
examples/subscribe/   Example subscriber with reply
```

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|---|---|---|
| `MQTT_BROKER_URL` | `tcp://localhost:1883` | MQTT broker URL |
| `MQTT_USERNAME` | `claimcheck` | MQTT username |
| `MQTT_PASSWORD` | `claimcheck123` | MQTT password |
| `MINIO_ENDPOINT` | `localhost:9000` | MinIO endpoint |
| `MINIO_ACCESS_KEY` | `minioadmin` | MinIO access key |
| `MINIO_SECRET_KEY` | `minioadmin` | MinIO secret key |
| `MINIO_BUCKET` | `claimcheck` | MinIO bucket name |
