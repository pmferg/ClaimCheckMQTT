package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/claimcheck/claimcheck-mqtt/pkg/publisher"
	"github.com/claimcheck/claimcheck-mqtt/pkg/storage"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()

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

	pub, err := publisher.New(publisher.Config{
		BrokerURL: envOr("MQTT_BROKER_URL", "tcp://localhost:1883"),
		ClientID:  "example-publisher",
		Username:  envOr("MQTT_USERNAME", "claimcheck"),
		Password:  envOr("MQTT_PASSWORD", "claimcheck123"),
		QoS:       1,
	}, store)
	if err != nil {
		slog.Error("publisher", "err", err)
		os.Exit(1)
	}
	defer pub.Close()

	payload := []byte(`{"temperature":22.5,"unit":"celsius","sensor":"living-room"}`)

	env, err := pub.Send(ctx, "sensors/temperature", "example", "application/json", payload)
	if err != nil {
		slog.Error("send", "err", err)
		os.Exit(1)
	}

	fmt.Printf("Published claim-check message\n  MPID:  %s\n  Topic: %s\n  Size:  %d bytes\n", env.MPID, env.Topic, env.PayloadSize)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
