package publisher

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/claimcheck/claimcheck-mqtt/pkg/claimcheck"
	"github.com/claimcheck/claimcheck-mqtt/pkg/storage"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Config holds publisher settings.
type Config struct {
	BrokerURL string
	ClientID  string
	Username  string
	Password  string
	QoS       byte
}

// Publisher wraps an MQTT client and a PayloadStore to send claim-check
// messages. It stores the payload first, then publishes the lightweight
// envelope to the broker.
type Publisher struct {
	client mqtt.Client
	store  storage.PayloadStore
	qos    byte
}

// New creates a connected Publisher.
func New(cfg Config, store storage.PayloadStore) (*Publisher, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(cfg.ClientID).
		SetAutoReconnect(true)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}

	client := mqtt.NewClient(opts)
	if tok := client.Connect(); tok.Wait() && tok.Error() != nil {
		return nil, fmt.Errorf("mqtt connect: %w", tok.Error())
	}
	slog.Info("publisher connected", "broker", cfg.BrokerURL)

	qos := cfg.QoS
	if qos == 0 {
		qos = 1
	}

	return &Publisher{client: client, store: store, qos: qos}, nil
}

// Send stores the payload in MinIO and publishes the claim-check envelope.
func (p *Publisher) Send(ctx context.Context, topic, source, contentType string, payload []byte) (claimcheck.Envelope, error) {
	env := claimcheck.NewEnvelope(topic, source, contentType, int64(len(payload)))

	if err := p.store.Put(ctx, env.MPID, bytes.NewReader(payload), int64(len(payload)), contentType); err != nil {
		return claimcheck.Envelope{}, fmt.Errorf("store payload: %w", err)
	}

	envBytes, err := env.Marshal()
	if err != nil {
		return claimcheck.Envelope{}, fmt.Errorf("marshal envelope: %w", err)
	}

	tok := p.client.Publish(topic, p.qos, false, envBytes)
	tok.Wait()
	if err := tok.Error(); err != nil {
		return claimcheck.Envelope{}, fmt.Errorf("mqtt publish: %w", err)
	}

	slog.Info("published", "mpid", env.MPID, "topic", topic, "size", len(payload))
	return env, nil
}

// SendStream is like Send but reads the payload from an io.Reader.
func (p *Publisher) SendStream(ctx context.Context, topic, source, contentType string, r io.Reader, size int64) (claimcheck.Envelope, error) {
	env := claimcheck.NewEnvelope(topic, source, contentType, size)

	if err := p.store.Put(ctx, env.MPID, r, size, contentType); err != nil {
		return claimcheck.Envelope{}, fmt.Errorf("store payload: %w", err)
	}

	envBytes, err := env.Marshal()
	if err != nil {
		return claimcheck.Envelope{}, fmt.Errorf("marshal envelope: %w", err)
	}

	tok := p.client.Publish(topic, p.qos, false, envBytes)
	tok.Wait()
	if err := tok.Error(); err != nil {
		return claimcheck.Envelope{}, fmt.Errorf("mqtt publish: %w", err)
	}

	slog.Info("published stream", "mpid", env.MPID, "topic", topic, "size", size)
	return env, nil
}

// Close disconnects from the broker.
func (p *Publisher) Close() {
	p.client.Disconnect(250)
}
