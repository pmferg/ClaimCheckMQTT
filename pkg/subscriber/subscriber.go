package subscriber

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/claimcheck/claimcheck-mqtt/pkg/claimcheck"
	"github.com/claimcheck/claimcheck-mqtt/pkg/storage"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Message is what the application callback receives after the subscriber
// has transparently resolved any claim-check payloads.
type Message struct {
	Topic       string
	Envelope    claimcheck.Envelope
	Payload     []byte
	RawEnvelope []byte
}

// MessageHandler is the application-level callback.
type MessageHandler func(msg Message)

// Config holds subscriber settings.
type Config struct {
	BrokerURL string
	ClientID  string
	Username  string
	Password  string
	QoS       byte
}

// Subscriber wraps an MQTT client and a PayloadStore. When it receives a
// message it checks whether the payload is a claim-check envelope; if so
// it fetches the real payload from storage before invoking the handler.
type Subscriber struct {
	client mqtt.Client
	store  storage.PayloadStore
	qos    byte
}

// New creates a connected Subscriber.
func New(cfg Config, store storage.PayloadStore) (*Subscriber, error) {
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
	slog.Info("subscriber connected", "broker", cfg.BrokerURL)

	qos := cfg.QoS
	if qos == 0 {
		qos = 1
	}

	return &Subscriber{client: client, store: store, qos: qos}, nil
}

// Subscribe registers a handler for the given MQTT topic filter.
// Claim-check envelopes are automatically resolved before the handler is
// invoked, so the handler always receives the full payload.
func (s *Subscriber) Subscribe(topic string, handler MessageHandler) error {
	tok := s.client.Subscribe(topic, s.qos, func(_ mqtt.Client, m mqtt.Message) {
		raw := m.Payload()

		if !claimcheck.IsClaimCheck(raw) {
			slog.Debug("non-claimcheck message, skipping", "topic", m.Topic())
			return
		}

		env, err := claimcheck.UnmarshalEnvelope(raw)
		if err != nil {
			slog.Error("bad envelope", "topic", m.Topic(), "err", err)
			return
		}

		payload, err := s.fetchPayload(env.MPID)
		if err != nil {
			slog.Error("fetch payload failed", "mpid", env.MPID, "err", err)
			return
		}

		handler(Message{
			Topic:       m.Topic(),
			Envelope:    env,
			Payload:     payload,
			RawEnvelope: raw,
		})
	})
	tok.Wait()
	if err := tok.Error(); err != nil {
		return fmt.Errorf("subscribe %s: %w", topic, err)
	}
	slog.Info("subscribed", "topic", topic)
	return nil
}

func (s *Subscriber) fetchPayload(mpid string) ([]byte, error) {
	rc, err := s.store.Get(context.Background(), mpid)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// Unsubscribe removes the subscription for the given topic.
func (s *Subscriber) Unsubscribe(topic string) error {
	tok := s.client.Unsubscribe(topic)
	tok.Wait()
	return tok.Error()
}

// Close disconnects from the broker.
func (s *Subscriber) Close() {
	s.client.Disconnect(250)
}
