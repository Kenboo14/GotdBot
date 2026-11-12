package controllers

import (
	"assterisk/gateway/applications/botgotd"
	"assterisk/gateway/applications/broker"
	"assterisk/gateway/applications/models"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
)

// sessionEventHub forwards lifecycle events from Redis to authenticated
// WebSocket clients. Redis remains the cross-process transport; WebSocket is
// only the push layer for dashboards and does not poll Telegram or Redis.
type sessionEventHub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
	token   string
}

func startSessionEventWebSocket(ctx context.Context, r *broker.Redis) {
	hub := &sessionEventHub{
		clients: make(map[*websocket.Conn]struct{}),
		token:   strings.TrimSpace(os.Getenv("SESSION_WS_TOKEN")),
	}

	go r.Listener(ctx, nil, "session:events", "session:ws", "session:events", func(_ context.Context, _ *botgotd.Bot, msg redis.XMessage) {
		payload, ok := msg.Values["payload"].(string)
		if !ok {
			return
		}
		var event models.PFromService
		if json.Unmarshal([]byte(payload), &event) == nil && event.Type == "session_status" {
			hub.broadcast(event)
		}
	})

	addr := strings.TrimSpace(os.Getenv("SESSION_WS_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:8081"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/session", hub.serve)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		hub.closeAll()
	}()

	go func() {
		log.Printf("[SessionWS] listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[SessionWS] stopped: %v", err)
		}
	}()
}

func (h *sessionEventHub) serve(w http.ResponseWriter, req *http.Request) {
	if h.token != "" && !validSessionWSToken(req, h.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	h.add(conn)
	defer h.remove(conn)

	// Keep the connection alive and detect client disconnects. All writes are
	// performed by broadcast, so there is only one writer per connection.
	for {
		if _, _, err := conn.Read(req.Context()); err != nil {
			return
		}
	}
}

func validSessionWSToken(req *http.Request, expected string) bool {
	auth := strings.TrimSpace(req.Header.Get("Authorization"))
	if strings.HasPrefix(auth, "Bearer ") && strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == expected {
		return true
	}
	return req.URL.Query().Get("token") == expected
}

func (h *sessionEventHub) add(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
}

func (h *sessionEventHub) remove(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	_ = conn.Close(websocket.StatusNormalClosure, "closed")
}

func (h *sessionEventHub) closeAll() {
	h.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		clients = append(clients, conn)
	}
	h.clients = make(map[*websocket.Conn]struct{})
	h.mu.Unlock()
	for _, conn := range clients {
		_ = conn.Close(websocket.StatusGoingAway, "server shutting down")
	}
}

func (h *sessionEventHub) broadcast(event models.PFromService) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}

	h.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		clients = append(clients, conn)
	}
	h.mu.Unlock()

	for _, conn := range clients {
		writeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := conn.Write(writeCtx, websocket.MessageText, payload)
		cancel()
		if err != nil {
			h.remove(conn)
		}
	}
}
