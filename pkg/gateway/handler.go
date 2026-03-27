package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/claimcheck/claimcheck-mqtt/pkg/claimcheck"
	"github.com/claimcheck/claimcheck-mqtt/pkg/storage"
)

// MQTTPublisher is the narrow interface the handler needs to push envelopes
// onto an MQTT broker.
type MQTTPublisher interface {
	Publish(topic string, payload []byte) error
}

// Handler exposes the webhook endpoints for the claim-check gateway.
type Handler struct {
	store     storage.PayloadStore
	publisher MQTTPublisher
	mux       *http.ServeMux
}

// NewHandler wires up routes and returns an http.Handler.
func NewHandler(store storage.PayloadStore, pub MQTTPublisher) *Handler {
	h := &Handler{store: store, publisher: pub}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ingest/{topic...}", h.handleIngest)
	mux.HandleFunc("GET /payload/{mpid}", h.handleGetPayload)
	mux.HandleFunc("GET /healthz", h.handleHealth)
	h.mux = mux
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// POST /ingest/{topic...}
//
// Headers:
//
//	X-Source         – optional source identifier
//	Content-Type     – MIME type of the payload (default application/octet-stream)
//
// Body: raw payload bytes.
func (h *Handler) handleIngest(w http.ResponseWriter, r *http.Request) {
	topic := r.PathValue("topic")
	if topic == "" {
		http.Error(w, "topic path required", http.StatusBadRequest)
		return
	}

	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	source := r.Header.Get("X-Source")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		http.Error(w, "empty payload", http.StatusBadRequest)
		return
	}

	env := claimcheck.NewEnvelope(topic, source, ct, int64(len(body)))

	if err := h.store.Put(r.Context(), env.MPID, bytesReader(body), int64(len(body)), ct); err != nil {
		slog.Error("store put failed", "mpid", env.MPID, "err", err)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	envBytes, err := env.Marshal()
	if err != nil {
		slog.Error("envelope marshal failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := h.publisher.Publish(topic, envBytes); err != nil {
		slog.Error("mqtt publish failed", "topic", topic, "err", err)
		http.Error(w, "publish error", http.StatusInternalServerError)
		return
	}

	slog.Info("ingested", "mpid", env.MPID, "topic", topic, "size", len(body))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(env)
}

// GET /payload/{mpid}
func (h *Handler) handleGetPayload(w http.ResponseWriter, r *http.Request) {
	mpid := r.PathValue("mpid")
	if mpid == "" {
		http.Error(w, "mpid required", http.StatusBadRequest)
		return
	}

	rc, err := h.store.Get(r.Context(), mpid)
	if err != nil {
		http.Error(w, fmt.Sprintf("payload not found: %v", err), http.StatusNotFound)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := io.Copy(w, rc); err != nil {
		slog.Error("stream payload failed", "mpid", mpid, "err", err)
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func bytesReader(b []byte) io.Reader {
	return io.NewSectionReader(readerAt(b), 0, int64(len(b)))
}

type readerAt []byte

func (r readerAt) ReadAt(p []byte, off int64) (int, error) {
	n := copy(p, r[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
