package claimcheck

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

const (
	EnvelopeType      = "claimcheck.v1"
	CallbackCloseType = "claimcheck.callback.close.v1"
	EnvelopeVersion   = "1.0"
)

// Envelope is the standardised outer message published to MQTT.
// It carries only metadata; the actual payload lives in object storage
// and is referenced by MPID.
type Envelope struct {
	Version       string `json:"version"`
	Type          string `json:"type"`
	MPID          string `json:"mpid"`
	Topic         string `json:"topic"`
	Source        string `json:"source,omitempty"`
	ContentType   string `json:"content_type"`
	PayloadSize   int64  `json:"payload_size"`
	Timestamp     int64  `json:"timestamp"`
	CallbackTopic string `json:"callback_topic,omitempty"`
}

// NewEnvelope creates an Envelope with a freshly generated MPID.
func NewEnvelope(topic, source, contentType string, payloadSize int64) Envelope {
	return Envelope{
		Version:     EnvelopeVersion,
		Type:        EnvelopeType,
		MPID:        GenerateMPID(),
		Topic:       topic,
		Source:      source,
		ContentType: contentType,
		PayloadSize: payloadSize,
		Timestamp:   time.Now().UnixMilli(),
	}
}

// NewCallbackCloseEnvelope creates a lightweight envelope that signals
// the publisher to stop listening on the callback topic.
func NewCallbackCloseEnvelope(callbackTopic, source string) Envelope {
	return Envelope{
		Version:   EnvelopeVersion,
		Type:      CallbackCloseType,
		Topic:     callbackTopic,
		Source:    source,
		Timestamp: time.Now().UnixMilli(),
	}
}

// Marshal serialises the envelope to JSON bytes.
func (e Envelope) Marshal() ([]byte, error) {
	return json.Marshal(e)
}

// UnmarshalEnvelope deserialises JSON bytes into an Envelope.
func UnmarshalEnvelope(data []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return env, nil
}

// IsClaimCheck returns true when the payload is a claim-check envelope
// (either a standard message or a callback close signal).
func IsClaimCheck(data []byte) bool {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Type == EnvelopeType || probe.Type == CallbackCloseType
}

// IsCallbackClose returns true when the envelope is a callback close signal.
func IsCallbackClose(data []byte) bool {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Type == CallbackCloseType
}

// GenerateMPID produces a unique Message Payload Identifier (UUIDv4).
func GenerateMPID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
