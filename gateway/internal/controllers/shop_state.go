package controllers

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

type shopPendingOrder struct {
	InvoiceID   string
	ExternalID  string
	ChatID      int64
	UserID      int64
	PackageID   string
	TotalAmount int64
	ExpiresAt   int64
	QRMsgID     int
}

type shopManager struct {
	mu        sync.Mutex
	pending   map[string]*shopPendingOrder
	fulfilled map[string]bool
	nonceSeen map[string]int64
}

func newShopManager() *shopManager {
	return &shopManager{
		pending:   make(map[string]*shopPendingOrder),
		fulfilled: make(map[string]bool),
		nonceSeen: make(map[string]int64),
	}
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
