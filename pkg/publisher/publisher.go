package publisher

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/pmferg/ClaimCheckMQTT/pkg/claimcheck"
	"github.com/pmferg/ClaimCheckMQTT/pkg/storage"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const defaultCallbackPrefix = "claimcheck/callbacks"

// Config holds publisher settings.
type Config struct {
	BrokerURL      string
	ClientID       string
	Username       string
	Password       string
	QoS            byte
	CallbackPrefix string // MQTT topic prefix for callback channels (default "claimcheck/callbacks")
}

// CallbackMessage is delivered on the channel returned by SendWithCallback.
type CallbackMessage struct {
	Envelope claimcheck.Envelope
	Payload  []byte
}

// Publisher connects directly to MQTT and MinIO to send claim-check messages.
type Publisher struct {
	client         mqtt.Client
	store          storage.PayloadStore
	qos            byte
	callbackPrefix string

	mu        sync.Mutex
	callbacks map[string]chan CallbackMessage // callback topic → channel
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

	prefix := cfg.CallbackPrefix
	if prefix == "" {
		prefix = defaultCallbackPrefix
	}

	return &Publisher{
		client:         client,
		store:          store,
		qos:            qos,
		callbackPrefix: prefix,
		callbacks:      make(map[string]chan CallbackMessage),
	}, nil
}

// Send stores the payload in MinIO and publishes the claim-check envelope.
func (p *Publisher) Send(ctx context.Context, topic, source, contentType string, payload []byte) (claimcheck.Envelope, error) {
	env := claimcheck.NewEnvelope(topic, source, contentType, int64(len(payload)))

	if err := p.store.Put(ctx, env.MPID, bytes.NewReader(payload), int64(len(payload)), contentType); err != nil {
		return claimcheck.Envelope{}, fmt.Errorf("store payload: %w", err)
	}

	if err := p.publish(env); err != nil {
		return claimcheck.Envelope{}, err
	}

	slog.Info("published", "mpid", env.MPID, "topic", topic, "size", len(payload))
	return env, nil
}

// SendWithCallback stores the payload, publishes the envelope with a unique
// callback topic, and returns a channel that receives response messages.
// The channel is closed when the subscriber sends a callback-close signal.
func (p *Publisher) SendWithCallback(ctx context.Context, topic, source, contentType string, payload []byte) (claimcheck.Envelope, <-chan CallbackMessage, error) {
	callbackTopic := fmt.Sprintf("%s/%s", p.callbackPrefix, claimcheck.GenerateMPID())

	ch := make(chan CallbackMessage, 16)
	p.mu.Lock()
	p.callbacks[callbackTopic] = ch
	p.mu.Unlock()

	// Subscribe to the callback topic before publishing so we don't miss
	// any responses that arrive immediately.
	tok := p.client.Subscribe(callbackTopic, p.qos, p.handleCallback)
	tok.Wait()
	if err := tok.Error(); err != nil {
		p.mu.Lock()
		delete(p.callbacks, callbackTopic)
		p.mu.Unlock()
		close(ch)
		return claimcheck.Envelope{}, nil, fmt.Errorf("subscribe callback: %w", err)
	}

	env := claimcheck.NewEnvelope(topic, source, contentType, int64(len(payload)))
	env.CallbackTopic = callbackTopic

	if err := p.store.Put(ctx, env.MPID, bytes.NewReader(payload), int64(len(payload)), contentType); err != nil {
		p.cleanupCallback(callbackTopic)
		return claimcheck.Envelope{}, nil, fmt.Errorf("store payload: %w", err)
	}

	if err := p.publish(env); err != nil {
		p.cleanupCallback(callbackTopic)
		return claimcheck.Envelope{}, nil, err
	}

	slog.Info("published with callback", "mpid", env.MPID, "topic", topic, "callback", callbackTopic)
	return env, ch, nil
}

// SendStream is like Send but reads the payload from an io.Reader.
func (p *Publisher) SendStream(ctx context.Context, topic, source, contentType string, r io.Reader, size int64) (claimcheck.Envelope, error) {
	env := claimcheck.NewEnvelope(topic, source, contentType, size)

	if err := p.store.Put(ctx, env.MPID, r, size, contentType); err != nil {
		return claimcheck.Envelope{}, fmt.Errorf("store payload: %w", err)
	}

	if err := p.publish(env); err != nil {
		return claimcheck.Envelope{}, err
	}

	slog.Info("published stream", "mpid", env.MPID, "topic", topic, "size", size)
	return env, nil
}

// Close disconnects from the broker and closes any open callback channels.
func (p *Publisher) Close() {
	p.mu.Lock()
	for topic, ch := range p.callbacks {
		p.client.Unsubscribe(topic)
		close(ch)
		delete(p.callbacks, topic)
	}
	p.mu.Unlock()
	p.client.Disconnect(250)
}

func (p *Publisher) publish(env claimcheck.Envelope) error {
	data, err := env.Marshal()
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	tok := p.client.Publish(env.Topic, p.qos, false, data)
	tok.Wait()
	if err := tok.Error(); err != nil {
		return fmt.Errorf("mqtt publish: %w", err)
	}
	return nil
}

// handleCallback is the MQTT message handler for all callback topics.
func (p *Publisher) handleCallback(_ mqtt.Client, m mqtt.Message) {
	raw := m.Payload()
	topic := m.Topic()

	if claimcheck.IsCallbackClose(raw) {
		slog.Info("callback closed by subscriber", "topic", topic)
		p.cleanupCallback(topic)
		return
	}

	env, err := claimcheck.UnmarshalEnvelope(raw)
	if err != nil {
		slog.Error("bad callback envelope", "topic", topic, "err", err)
		return
	}

	payload, err := p.fetchPayload(env.MPID)
	if err != nil {
		slog.Error("fetch callback payload failed", "mpid", env.MPID, "err", err)
		return
	}

	p.mu.Lock()
	ch, ok := p.callbacks[topic]
	p.mu.Unlock()

	if ok {
		ch <- CallbackMessage{Envelope: env, Payload: payload}
	}
}

func (p *Publisher) fetchPayload(mpid string) ([]byte, error) {
	rc, err := p.store.Get(context.Background(), mpid)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func (p *Publisher) cleanupCallback(topic string) {
	p.client.Unsubscribe(topic)
	p.mu.Lock()
	if ch, ok := p.callbacks[topic]; ok {
		close(ch)
		delete(p.callbacks, topic)
	}
	p.mu.Unlock()
}
