package subscriber

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/pmferg/ClaimCheckMQTT/pkg/claimcheck"
	"github.com/pmferg/ClaimCheckMQTT/pkg/storage"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Message is what the application callback receives after the subscriber
// has transparently resolved claim-check payloads. If the original message
// included a callback topic, the handler can use Reply and CloseCallback
// to send responses back to the publisher.
type Message struct {
	Topic       string
	Envelope    claimcheck.Envelope
	Payload     []byte
	RawEnvelope []byte
	reply       func(ctx context.Context, contentType string, payload []byte) error
	closeCb     func() error
}

// HasCallback returns true when the publisher requested a callback channel.
func (m *Message) HasCallback() bool {
	return m.Envelope.CallbackTopic != ""
}

// Reply sends a claim-check response to the publisher's callback topic.
// The payload is stored in MinIO and only the envelope travels over MQTT.
func (m *Message) Reply(ctx context.Context, contentType string, payload []byte) error {
	if m.reply == nil {
		return fmt.Errorf("no callback topic on this message")
	}
	return m.reply(ctx, contentType, payload)
}

// CloseCallback sends a close signal telling the publisher to stop
// listening on the callback topic and close its channel.
func (m *Message) CloseCallback() error {
	if m.closeCb == nil {
		return fmt.Errorf("no callback topic on this message")
	}
	return m.closeCb()
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
// invoked, so the handler always receives the full payload. If the envelope
// carries a callback topic, the Message's Reply and CloseCallback methods
// are wired up and ready to use.
func (s *Subscriber) Subscribe(topic string, handler MessageHandler) error {
	tok := s.client.Subscribe(topic, s.qos, func(_ mqtt.Client, m mqtt.Message) {
		raw := m.Payload()

		if !claimcheck.IsClaimCheck(raw) {
			slog.Debug("non-claimcheck message, skipping", "topic", m.Topic())
			return
		}

		if claimcheck.IsCallbackClose(raw) {
			slog.Debug("ignoring callback close on main subscription", "topic", m.Topic())
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

		msg := Message{
			Topic:       m.Topic(),
			Envelope:    env,
			Payload:     payload,
			RawEnvelope: raw,
		}

		if env.CallbackTopic != "" {
			msg.reply = s.makeReplyFunc(env.CallbackTopic, env.Source)
			msg.closeCb = s.makeCloseFunc(env.CallbackTopic, env.Source)
		}

		handler(msg)
	})
	tok.Wait()
	if err := tok.Error(); err != nil {
		return fmt.Errorf("subscribe %s: %w", topic, err)
	}
	slog.Info("subscribed", "topic", topic)
	return nil
}

func (s *Subscriber) makeReplyFunc(callbackTopic, source string) func(ctx context.Context, contentType string, payload []byte) error {
	return func(ctx context.Context, contentType string, payload []byte) error {
		env := claimcheck.NewEnvelope(callbackTopic, source, contentType, int64(len(payload)))

		if err := s.store.Put(ctx, env.MPID, bytes.NewReader(payload), int64(len(payload)), contentType); err != nil {
			return fmt.Errorf("store callback payload: %w", err)
		}

		data, err := env.Marshal()
		if err != nil {
			return fmt.Errorf("marshal callback envelope: %w", err)
		}

		tok := s.client.Publish(callbackTopic, s.qos, false, data)
		tok.Wait()
		if err := tok.Error(); err != nil {
			return fmt.Errorf("publish callback: %w", err)
		}

		slog.Info("callback reply sent", "mpid", env.MPID, "callback_topic", callbackTopic, "size", len(payload))
		return nil
	}
}

func (s *Subscriber) makeCloseFunc(callbackTopic, source string) func() error {
	return func() error {
		env := claimcheck.NewCallbackCloseEnvelope(callbackTopic, source)
		data, err := env.Marshal()
		if err != nil {
			return fmt.Errorf("marshal callback close: %w", err)
		}

		tok := s.client.Publish(callbackTopic, s.qos, false, data)
		tok.Wait()
		if err := tok.Error(); err != nil {
			return fmt.Errorf("publish callback close: %w", err)
		}

		slog.Info("callback closed", "callback_topic", callbackTopic)
		return nil
	}
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
