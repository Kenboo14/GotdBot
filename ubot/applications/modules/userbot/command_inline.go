package userbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gotd/td/crypto"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// maxInlineFloodWait defines the maximum duration to wait and retry an inline query.
// Longer flood waits fail fast to avoid blocking userbot workers.
const maxInlineFloodWait = 15 * time.Second

var randomFallbackCounter int64

// inlineRandomID generates a cryptographically uniform 64-bit random integer
// for Telegram MTProto message deduplication via gotd/td/crypto.
// The uniform 63-bit positive space (2^63 - 1) provides negligible collision
// probability (probabilistic uniqueness). If the entropy source fails,
// it falls back to a hybrid timestamp XOR atomic sequence.
func inlineRandomID() int64 {
	id, err := crypto.RandInt64(crypto.DefaultRand())
	if err == nil && id != 0 {
		return id
	}
	return time.Now().UnixNano() ^ (atomic.AddInt64(&randomFallbackCounter, 1) << 16)
}

// ──────────────────────────────────────────────────────
// sendInlineHelp — shortcut for !help via inline bot
// ──────────────────────────────────────────────────────

func (s *Service) sendInlineHelp(ctx context.Context, client *telegram.Client, target tg.InputPeerClass) error {
	return s.sendInlineQuery(ctx, client, target, "assist_help")
}

// ──────────────────────────────────────────────────────
// sendInlineQuery — core inline get+send pipeline
//
// Optimizations & Safety:
//  1. Handles both BotInlineResult and BotInlineMediaResult via GetID().
//  2. Collision-free MTProto RandomID via gotd/td/crypto.
//  3. Bounded FloodWait detection and single-retry backoff (<= 15s).
//  4. Per-step latency instrumentation.
//  5. RWMutex cached reads for bot InputUser (zero lock contention).
// ──────────────────────────────────────────────────────

func (s *Service) sendInlineQuery(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, query string) error {
	cmdStart := time.Now()

	// Step 1: Resolve inline bot InputUser (cached after first call per client)
	resolveStart := time.Now()
	inputBot, err := s.inlineBotUser(ctx, client)
	if err != nil {
		return fmt.Errorf("resolve inline bot: %w", err)
	}
	resolveDur := time.Since(resolveStart)

	// Step 2: GetInlineBotResults RPC
	getStart := time.Now()
	results, err := s.getInlineBotResultsWithRetry(ctx, client, inputBot, target, query)
	if err != nil {
		return fmt.Errorf("getInlineBotResults: %w", err)
	}
	getDur := time.Since(getStart)

	if len(results.Results) == 0 {
		return fmt.Errorf("bot tidak mengembalikan hasil inline")
	}

	// Step 3: Extract result ID — works for BotInlineResult AND BotInlineMediaResult
	// via the BotInlineResultClass.GetID() interface method.
	resultID := results.Results[0].GetID()

	// Step 4: SendInlineBotResult RPC
	sendStart := time.Now()
	randomID := inlineRandomID()
	_, err = s.sendInlineBotResultWithRetry(ctx, client, target, results.QueryID, resultID, randomID)
	sendDur := time.Since(sendStart)

	totalDur := time.Since(cmdStart)
	log.Printf("[inline] query=%q resolve=%v get=%v send=%v total=%v err=%v",
		truncateQuery(query), resolveDur, getDur, sendDur, totalDur, err)

	return err
}

// truncateQuery shortens a query string for safe logging (no user content leak).
func truncateQuery(q string) string {
	if len(q) > 30 {
		return q[:27] + "..."
	}
	return q
}

// ──────────────────────────────────────────────────────
// RPC helpers with FloodWait retry
// ──────────────────────────────────────────────────────

func (s *Service) getInlineBotResultsWithRetry(
	ctx context.Context,
	client *telegram.Client,
	bot *tg.InputUser,
	peerTarget tg.InputPeerClass,
	query string,
) (*tg.MessagesBotResults, error) {
	raw := client.API()
	req := &tg.MessagesGetInlineBotResultsRequest{
		Bot:    bot,
		Peer:   peerTarget,
		Query:  query,
		Offset: "",
	}

	results, err := raw.MessagesGetInlineBotResults(ctx, req)
	if err != nil {
		if wait, ok := tgerr.AsFloodWait(err); ok {
			if wait > maxInlineFloodWait {
				return nil, fmt.Errorf("inline get results flood wait (%v) melebihi batas maksimum (%v): %w", wait, maxInlineFloodWait, err)
			}
			log.Printf("[inline] GetInlineBotResults FloodWait %v, waiting...", wait)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait + 200*time.Millisecond):
			}
			return raw.MessagesGetInlineBotResults(ctx, req)
		}
		// If the bot InputUser is stale (e.g. access hash changed), evict and retry once
		if isAccessHashError(err) {
			s.evictInlineBot(client)
			newBot, resolveErr := s.inlineBotUser(ctx, client)
			if resolveErr != nil {
				return nil, err // return original error
			}
			req.Bot = newBot
			return raw.MessagesGetInlineBotResults(ctx, req)
		}
		return nil, err
	}
	return results, nil
}

func (s *Service) sendInlineBotResultWithRetry(
	ctx context.Context,
	client *telegram.Client,
	peerTarget tg.InputPeerClass,
	queryID int64,
	resultID string,
	randomID int64,
) (tg.UpdatesClass, error) {
	raw := client.API()
	req := &tg.MessagesSendInlineBotResultRequest{
		Peer:     peerTarget,
		QueryID:  queryID,
		ID:       resultID,
		RandomID: randomID,
	}

	result, err := raw.MessagesSendInlineBotResult(ctx, req)
	if err != nil {
		if wait, ok := tgerr.AsFloodWait(err); ok {
			if wait > maxInlineFloodWait {
				return nil, fmt.Errorf("inline send result flood wait (%v) melebihi batas maksimum (%v): %w", wait, maxInlineFloodWait, err)
			}
			log.Printf("[inline] SendInlineBotResult FloodWait %v, waiting...", wait)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait + 200*time.Millisecond):
			}
			return raw.MessagesSendInlineBotResult(ctx, req)
		}
		return nil, err
	}
	return result, nil
}

// isAccessHashError detects Telegram errors indicating a stale access hash.
func isAccessHashError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "BOT_INVALID") ||
		strings.Contains(msg, "ACCESS_HASH") ||
		strings.Contains(msg, "USER_ID_INVALID")
}

// ──────────────────────────────────────────────────────
// inlineBotUser — cached per-client InputUser for the Gateway bot
//
// Optimizations:
//  1. Uses RWMutex for cache reads (concurrent reads don't block).
//  2. Caches result permanently per client (evicted on client cleanup
//     or access hash error via evictInlineBot).
// ──────────────────────────────────────────────────────

func (s *Service) inlineBotUser(ctx context.Context, client *telegram.Client) (*tg.InputUser, error) {
	username, err := s.botUsername(ctx)
	if err != nil {
		return nil, err
	}

	// Fast path: RLock for concurrent reads
	s.inlineMu.RLock()
	if cachedBot := s.inlineBots[client]; cachedBot != nil {
		cached := *cachedBot
		s.inlineMu.RUnlock()
		return &cached, nil
	}
	s.inlineMu.RUnlock()

	// Slow path: resolve and cache under write lock
	resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
	if err != nil {
		return nil, err
	}
	botPeer, err := peer.EntitiesFromResult(resolved).ExtractPeer(resolved.Peer)
	if err != nil {
		return nil, err
	}
	inputPeer, ok := botPeer.(*tg.InputPeerUser)
	if !ok {
		return nil, fmt.Errorf("BOT_TOKEN bukan akun bot")
	}
	resolvedBot := &tg.InputUser{UserID: inputPeer.UserID, AccessHash: inputPeer.AccessHash}

	s.inlineMu.Lock()
	if s.inlineBots == nil {
		s.inlineBots = make(map[*telegram.Client]*tg.InputUser)
	}
	s.inlineBots[client] = resolvedBot
	cached := *resolvedBot
	s.inlineMu.Unlock()
	return &cached, nil
}

// evictInlineBot removes the cached bot InputUser for this client, forcing re-resolve.
func (s *Service) evictInlineBot(client *telegram.Client) {
	s.inlineMu.Lock()
	delete(s.inlineBots, client)
	s.inlineMu.Unlock()
}

// preWarmInlineBot resolves and caches the gateway bot InputUser in background
// right after a userbot client connects, so the first !help / !server / !save -cb
// doesn't pay the cold-start penalty.
//
// Jitter (0-3s) and clientCtx binding prevent startup storms across hundreds of accounts.
func (s *Service) preWarmInlineBot(clientCtx context.Context, client *telegram.Client) {
	go func() {
		// Stagger execution across multiple concurrently booting accounts
		jitter := time.Duration(atomic.AddUint64(&s.nextGen, 1)%30) * 100 * time.Millisecond
		select {
		case <-clientCtx.Done():
			return
		case <-time.After(jitter):
		}

		ctx, cancel := context.WithTimeout(clientCtx, 15*time.Second)
		defer cancel()
		if _, err := s.inlineBotUser(ctx, client); err != nil {
			log.Printf("[inline] preWarmInlineBot gagal: %v", err)
		}
	}()
}

// ──────────────────────────────────────────────────────
// botUsername — resolves BOT_TOKEN → username via Bot API getMe
//
// Optimizations:
//  1. Uses RWMutex for cached reads.
//  2. Single shared HTTP client with connection pooling.
//  3. Result cached permanently (bot username never changes).
// ──────────────────────────────────────────────────────

// sharedHTTPClient is reused across all botUsername() calls to avoid
// creating a new TCP/TLS connection each time. Since the only target is
// api.telegram.org, a single persistent client is enough.
var sharedHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
	},
}

func (s *Service) botUsername(ctx context.Context) (string, error) {
	if strings.TrimSpace(s.botToken) == "" {
		return "", fmt.Errorf("BOT_TOKEN belum dikonfigurasi")
	}

	// Fast path: RLock
	s.inlineMu.RLock()
	if s.botUsernameCache != "" {
		username := s.botUsernameCache
		s.inlineMu.RUnlock()
		return username, nil
	}
	s.inlineMu.RUnlock()

	// Slow path: HTTP call + write lock
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.telegram.org/bot"+s.botToken+"/getMe", nil)
	if err != nil {
		return "", err
	}
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var answer struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return "", err
	}
	if !answer.OK || strings.TrimSpace(answer.Result.Username) == "" {
		return "", fmt.Errorf("BOT_TOKEN tidak dapat di-resolve")
	}
	username := strings.TrimPrefix(strings.TrimSpace(answer.Result.Username), "@")
	s.inlineMu.Lock()
	if s.botUsernameCache == "" {
		s.botUsernameCache = username
	}
	username = s.botUsernameCache
	s.inlineMu.Unlock()
	return username, nil
}
