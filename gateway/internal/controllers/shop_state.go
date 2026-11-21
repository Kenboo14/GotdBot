package controllers

import (
	"assterisk/gateway/internal/databases"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	shopRateLimitWindow = 5 * time.Minute
	shopRateLimitMax    = 3
	shopMaxQty          = 24
	shopMinPriceIDR     = 100
	shopMaxPriceIDR     = 100000000
)

type shopItemPricing struct {
	PriceIDR    int64
	DiscountPct int
}

type shopPendingOrder struct {
	InvoiceID   string
	ExternalID  string
	ChatID      int64
	UserID      int64
	PackageID   string
	Qty         int
	TotalAmount int64
	ExpiresAt   int64
	QRMsgID     int
}

type shopManager struct {
	mu           sync.Mutex
	db           *databases.DataManager
	pending      map[string]*shopPendingOrder
	fulfilled    map[string]bool
	nonceSeen    map[string]int64
	pricing      map[string]shopItemPricing
	orderHistory map[int64][]int64
}

func defaultShopPricingMap() map[string]shopItemPricing {
	return map[string]shopItemPricing{
		"pro":     {PriceIDR: 45000, DiscountPct: 0},
		"proplus": {PriceIDR: 65000, DiscountPct: 0},
	}
}

func newShopManager(db *databases.DataManager) *shopManager {
	s := &shopManager{
		db:           db,
		pending:      make(map[string]*shopPendingOrder),
		fulfilled:    make(map[string]bool),
		nonceSeen:    make(map[string]int64),
		pricing:      defaultShopPricingMap(),
		orderHistory: make(map[int64][]int64),
	}
	s.initPricingDB()
	return s
}

func (s *shopManager) initPricingDB() {
	if s == nil || s.db == nil {
		return
	}
	sq := s.db.GetSQLiteDB()
	if sq == nil {
		return
	}
	_, err := sq.Exec(`CREATE TABLE IF NOT EXISTS shop_settings (
		item_id TEXT PRIMARY KEY,
		price_idr INTEGER NOT NULL,
		discount_pct INTEGER NOT NULL DEFAULT 0,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`)
	if err != nil {
		return
	}
	rows, err := sq.Query(`SELECT item_id, price_idr, discount_pct FROM shop_settings;`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var price int64
		var disc int
		if err := rows.Scan(&id, &price, &disc); err != nil {
			continue
		}
		if _, ok := s.pricing[id]; !ok {
			continue
		}
		if price >= shopMinPriceIDR && price <= shopMaxPriceIDR {
			if disc < 0 {
				disc = 0
			}
			if disc > 99 {
				disc = 99
			}
			s.pricing[id] = shopItemPricing{
				PriceIDR:    price,
				DiscountPct: disc,
			}
		}
	}
}

func (s *shopManager) persistPricingLocked(id string, pr shopItemPricing) {
	if s == nil || s.db == nil {
		return
	}
	sq := s.db.GetSQLiteDB()
	if sq == nil {
		return
	}
	_, _ = sq.Exec(`INSERT INTO shop_settings (item_id, price_idr, discount_pct, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(item_id) DO UPDATE SET
			price_idr = excluded.price_idr,
			discount_pct = excluded.discount_pct,
			updated_at = CURRENT_TIMESTAMP;`,
		id, pr.PriceIDR, pr.DiscountPct,
	)
}

func (s *shopManager) getPricing(baseID string) shopItemPricing {
	defaults := defaultShopPricingMap()
	def := defaults[baseID]
	if s == nil {
		return def
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if pr, ok := s.pricing[baseID]; ok && pr.PriceIDR >= shopMinPriceIDR {
		return pr
	}
	return def
}

func (s *shopManager) setItemPrice(baseID string, priceIDR int64) error {
	if priceIDR < shopMinPriceIDR || priceIDR > shopMaxPriceIDR {
		return fmt.Errorf("harga harus antara Rp %d sampai Rp %d", shopMinPriceIDR, shopMaxPriceIDR)
	}
	if baseID != "pro" && baseID != "proplus" {
		return fmt.Errorf("item shop tidak valid: %s", baseID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.pricing[baseID]
	if cur.PriceIDR < shopMinPriceIDR {
		cur = defaultShopPricingMap()[baseID]
	}
	cur.PriceIDR = priceIDR
	s.pricing[baseID] = cur
	s.persistPricingLocked(baseID, cur)
	return nil
}

func (s *shopManager) setDiscount(target string, pct int) error {
	if pct < 0 || pct > 99 {
		return fmt.Errorf("diskon harus antara 0%% sampai 99%%")
	}
	var ids []string
	switch target {
	case "pro":
		ids = []string{"pro"}
	case "proplus":
		ids = []string{"proplus"}
	case "all":
		ids = []string{"pro", "proplus"}
	default:
		return fmt.Errorf("target diskon tidak valid: %s", target)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	defaults := defaultShopPricingMap()
	for _, id := range ids {
		cur := s.pricing[id]
		if cur.PriceIDR < shopMinPriceIDR {
			cur = defaults[id]
		}
		cur.DiscountPct = pct
		s.pricing[id] = cur
		s.persistPricingLocked(id, cur)
	}
	return nil
}

func pruneTimestampsWithinWindow(tsList []int64, cutoff int64) []int64 {
	if len(tsList) == 0 {
		return nil
	}
	out := tsList[:0]
	for _, ts := range tsList {
		if ts > cutoff {
			out = append(out, ts)
		}
	}
	return out
}

func (s *shopManager) checkOrderRateLimit(buyerID int64, now time.Time) (bool, int64) {
	if s == nil || buyerID <= 0 {
		return true, 0
	}
	nowUnix := now.Unix()
	winSec := int64(shopRateLimitWindow.Seconds())
	cutoff := nowUnix - winSec

	s.mu.Lock()
	defer s.mu.Unlock()
	recent := pruneTimestampsWithinWindow(s.orderHistory[buyerID], cutoff)
	if len(recent) == 0 {
		delete(s.orderHistory, buyerID)
		return true, 0
	}
	s.orderHistory[buyerID] = recent
	if len(recent) < shopRateLimitMax {
		return true, 0
	}
	wait := (recent[0] + winSec) - nowUnix
	if wait < 1 {
		wait = 1
	}
	return false, wait
}

func (s *shopManager) recordOrderCreation(buyerID int64, now time.Time) {
	if s == nil || buyerID <= 0 {
		return
	}
	nowUnix := now.Unix()
	cutoff := nowUnix - int64(shopRateLimitWindow.Seconds())

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.orderHistory) > 4096 {
		for uid, list := range s.orderHistory {
			cleaned := pruneTimestampsWithinWindow(list, cutoff)
			if len(cleaned) == 0 {
				delete(s.orderHistory, uid)
			} else {
				s.orderHistory[uid] = cleaned
			}
		}
	}
	recent := pruneTimestampsWithinWindow(s.orderHistory[buyerID], cutoff)
	recent = append(recent, nowUnix)
	s.orderHistory[buyerID] = recent
}

func (s *shopManager) trackPending(ord *shopPendingOrder) {
	if s == nil || ord == nil {
		return
	}
	s.mu.Lock()
	s.pending[ord.InvoiceID] = ord
	s.mu.Unlock()
}

func (s *shopManager) getPending(invoiceID string) *shopPendingOrder {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ord, ok := s.pending[invoiceID]; ok {
		cp := *ord
		return &cp
	}
	return nil
}

func (s *shopManager) removePending(invoiceID string) *shopPendingOrder {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ord := s.pending[invoiceID]
	delete(s.pending, invoiceID)
	if ord == nil {
		return nil
	}
	cp := *ord
	return &cp
}

func (s *shopManager) getActiveForBuyer(buyerID int64, now time.Time) *shopPendingOrder {
	if s == nil || buyerID <= 0 {
		return nil
	}
	nowUnix := now.Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ord := range s.pending {
		if ord.ExpiresAt <= nowUnix {
			delete(s.pending, id)
			continue
		}
		if ord.UserID == buyerID && !s.fulfilled[id] {
			cp := *ord
			return &cp
		}
	}
	return nil
}

func (s *shopManager) snapshotPending(now time.Time) []*shopPendingOrder {
	if s == nil {
		return nil
	}
	nowUnix := now.Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*shopPendingOrder
	for id, ord := range s.pending {
		if ord.ExpiresAt+150 <= nowUnix {
			delete(s.pending, id)
			continue
		}
		cp := *ord
		out = append(out, &cp)
	}
	return out
}

func (s *shopManager) claimFulfillOnce(invoiceID string) bool {
	if s == nil || invoiceID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fulfilled[invoiceID] {
		return false
	}
	s.fulfilled[invoiceID] = true
	if len(s.fulfilled) > 2048 {
		clear(s.fulfilled)
		s.fulfilled[invoiceID] = true
	}
	delete(s.pending, invoiceID)
	return true
}

func (s *shopManager) checkCallbackReplay(tsStr, nonce string, now time.Time) bool {
	tsUnix, err := strconv.ParseInt(strings.TrimSpace(tsStr), 10, 64)
	if err != nil {
		return false
	}
	nonce = strings.TrimSpace(nonce)
	if len(nonce) < 16 || len(nonce) > 128 {
		return false
	}
	nowUnix := now.Unix()
	diff := nowUnix - tsUnix
	if diff < 0 {
		diff = -diff
	}
	if diff > 60 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.nonceSeen) > 2048 {
		for k, exp := range s.nonceSeen {
			if exp < nowUnix {
				delete(s.nonceSeen, k)
			}
		}
	}
	if exp, exists := s.nonceSeen[nonce]; exists && exp >= nowUnix {
		return false
	}
	s.nonceSeen[nonce] = nowUnix + 120
	return true
}
