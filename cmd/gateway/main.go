package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	gw "github.com/claimcheck/claimcheck-mqtt/pkg/gateway"
	"github.com/claimcheck/claimcheck-mqtt/pkg/storage"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	store, err := storage.NewMinIO(ctx, storage.MinIOConfig{
		Endpoint:  envOrDefault("MINIO_ENDPOINT", "minio:9000"),
		AccessKey: envOrDefault("MINIO_ACCESS_KEY", "minioadmin"),
		SecretKey: envOrDefault("MINIO_SECRET_KEY", "minioadmin"),
		Bucket:    envOrDefault("MINIO_BUCKET", "claimcheck"),
		UseSSL:    os.Getenv("MINIO_USE_SSL") == "true",
	})
	if err != nil {
		slog.Error("storage init failed", "err", err)
		os.Exit(1)
	}

	mqttPub, err := newMQTTPublisher(
		envOrDefault("MQTT_BROKER_URL", "tcp://mqtt:1883"),
		envOrDefault("MQTT_CLIENT_ID", "claimcheck-gateway"),
		os.Getenv("MQTT_USERNAME"),
		os.Getenv("MQTT_PASSWORD"),
	)
	if err != nil {
		slog.Error("mqtt init failed", "err", err)
		os.Exit(1)
	}
	defer mqttPub.Close()

	handler := gw.NewHandler(store, mqttPub)

	addr := envOrDefault("LISTEN_ADDR", ":8080")
	srv := &http.Server{Addr: addr, Handler: handler}

	go func() {
		slog.Info("gateway listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	srv.Shutdown(shutCtx)
}

// mqttAdapter implements gateway.MQTTPublisher using the paho client.
type mqttAdapter struct {
	client mqtt.Client
}

func newMQTTPublisher(brokerURL, clientID, username, password string) (*mqttAdapter, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(2 * time.Second)

	if username != "" {
		opts.SetUsername(username)
		opts.SetPassword(password)
	}

	client := mqtt.NewClient(opts)
	if tok := client.Connect(); tok.Wait() && tok.Error() != nil {
		return nil, fmt.Errorf("mqtt connect: %w", tok.Error())
	}
	slog.Info("mqtt connected", "broker", brokerURL)
	return &mqttAdapter{client: client}, nil
}

func (a *mqttAdapter) Publish(topic string, payload []byte) error {
	tok := a.client.Publish(topic, 1, false, payload)
	tok.Wait()
	return tok.Error()
}

func (a *mqttAdapter) Close() {
	a.client.Disconnect(250)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
