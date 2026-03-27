package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/claimcheck/claimcheck-mqtt/pkg/storage"
	"github.com/claimcheck/claimcheck-mqtt/pkg/subscriber"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	store, err := storage.NewMinIO(ctx, storage.MinIOConfig{
		Endpoint:  envOr("MINIO_ENDPOINT", "localhost:9000"),
		AccessKey: envOr("MINIO_ACCESS_KEY", "minioadmin"),
		SecretKey: envOr("MINIO_SECRET_KEY", "minioadmin"),
		Bucket:    envOr("MINIO_BUCKET", "claimcheck"),
	})
	if err != nil {
		slog.Error("storage", "err", err)
		os.Exit(1)
	}

	sub, err := subscriber.New(subscriber.Config{
		BrokerURL: envOr("MQTT_BROKER_URL", "tcp://localhost:1883"),
		ClientID:  "example-subscriber",
		Username:  envOr("MQTT_USERNAME", "claimcheck"),
		Password:  envOr("MQTT_PASSWORD", "claimcheck123"),
		QoS:       1,
	}, store)
	if err != nil {
		slog.Error("subscriber", "err", err)
		os.Exit(1)
	}
	defer sub.Close()

	topic := envOr("TOPIC", "sensors/#")

	err = sub.Subscribe(topic, func(msg subscriber.Message) {
		fmt.Printf("Received claim-check message\n  MPID:    %s\n  Topic:   %s\n  Source:  %s\n  Size:    %d bytes\n  Payload: %s\n\n",
			msg.Envelope.MPID,
			msg.Topic,
			msg.Envelope.Source,
			msg.Envelope.PayloadSize,
			string(msg.Payload),
		)
	})
	if err != nil {
		slog.Error("subscribe", "err", err)
		os.Exit(1)
	}

	fmt.Printf("Listening on %s  (ctrl-c to quit)\n", topic)
	<-ctx.Done()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
