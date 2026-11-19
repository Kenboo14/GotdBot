package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotd/td/tgerr"
)

type pacedSlotState struct {
	target any
	bot    *botgotd.Bot
	sendFn func(context.Context, *botgotd.SendMessageParams) (*botgotd.Message, error)
	editFn func(context.Context, *botgotd.EditMessageTextParams) (*botgotd.Message, error)
}

func (d *monitorDispatcher) waitPace(ctx context.Context) error {
	for {
		now := time.Now()
		d.mu.Lock()
		if d.cfg.refillEvery > 0 {
			if d.tokens >= d.cfg.burst {
				d.tokens = d.cfg.burst
				d.lastRefill = now
			} else if d.lastRefill.IsZero() {
				d.lastRefill = now
			} else if elapsed := now.Sub(d.lastRefill); elapsed >= d.cfg.refillEvery {
				added := int(elapsed / d.cfg.refillEvery)
				d.tokens += added
				if d.tokens >= d.cfg.burst {
					d.tokens = d.cfg.burst
					d.lastRefill = now
				} else {
					d.lastRefill = d.lastRefill.Add(time.Duration(added) * d.cfg.refillEvery)
				}
			}
		}

		var waitDur time.Duration
		if !d.cooldownUntil.IsZero() && d.cooldownUntil.After(now) {
			waitDur = d.cooldownUntil.Sub(now)
		}
		if d.cfg.minGap > 0 && !d.lastSend.IsZero() {
			if gap := d.cfg.minGap - now.Sub(d.lastSend); gap > waitDur {
				waitDur = gap
			}
		}
		if d.cfg.refillEvery > 0 && d.tokens <= 0 {
			if rem := d.cfg.refillEvery - now.Sub(d.lastRefill); rem > waitDur {
				waitDur = rem
			}
		}
		if waitDur <= 0 {
			if d.cfg.refillEvery > 0 && d.tokens > 0 {
				d.tokens--
			}
			d.lastSend = now
			d.mu.Unlock()
			return nil
		}
		d.mu.Unlock()

		timer := time.NewTimer(waitDur)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (d *monitorDispatcher) withPacedSlot(ctx context.Context, fn func(context.Context, pacedSlotState) (*botgotd.Message, error)) (*botgotd.Message, error) {
	select {
	case d.sendSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-d.sendSem }()

	if err := d.waitPace(ctx); err != nil {
		return nil, err
	}

	d.mu.Lock()
	state := pacedSlotState{
		target: d.chatTarget,
		bot:    d.b,
		sendFn: d.sendFn,
		editFn: d.editFn,
	}
	d.mu.Unlock()

	opCtx, cancel := context.WithTimeout(ctx, d.cfg.sendTimeout)
	defer cancel()
	return fn(opCtx, state)
}

func (d *monitorDispatcher) execSend(ctx context.Context, threadID int, text string) (*botgotd.Message, error) {
	return d.withPacedSlot(ctx, func(opCtx context.Context, st pacedSlotState) (*botgotd.Message, error) {
		if st.target == nil || (st.bot == nil && st.sendFn == nil) {
			return nil, nil
		}
		params := &botgotd.SendMessageParams{
			ChatID:          st.target,
			MessageThreadID: threadID,
			Text:            text,
			ParseMode:       botgotd.ParseModeHTML,
		}
		if st.sendFn != nil {
			return st.sendFn(opCtx, params)
		}
		return st.bot.SendMessage(opCtx, params)
	})
}

func (d *monitorDispatcher) execEdit(ctx context.Context, msgID int, text string) (*botgotd.Message, error) {
	return d.withPacedSlot(ctx, func(opCtx context.Context, st pacedSlotState) (*botgotd.Message, error) {
		if st.target == nil || (st.bot == nil && st.editFn == nil) {
			return nil, nil
		}
		params := &botgotd.EditMessageTextParams{
			ChatID:    st.target,
			MessageID: msgID,
			Text:      text,
			ParseMode: botgotd.ParseModeHTML,
		}
		if st.editFn != nil {
			return st.editFn(opCtx, params)
		}
		return st.bot.EditMessageText(opCtx, params)
	})
}

func (d *monitorDispatcher) recordCooldown(wait time.Duration) {
	total := wait + d.cfg.floodExtraWait
	if total > d.cfg.maxWaitCap {
		total = d.cfg.maxWaitCap
	}
	until := time.Now().Add(total)
	d.mu.Lock()
	if until.After(d.cooldownUntil) {
		d.cooldownUntil = until
	}
	d.mu.Unlock()
}

func (d *monitorDispatcher) isTopicBad(topicID int) bool {
	if topicID <= 0 {
		return false
	}
	now := time.Now()
	d.mu.Lock()
	until, ok := d.badTopics[topicID]
	if ok && !now.Before(until) {
		delete(d.badTopics, topicID)
		ok = false
	}
	d.mu.Unlock()
	return ok
}

func (d *monitorDispatcher) markTopicBad(topicID int) {
	if topicID <= 0 {
		return
	}
	d.mu.Lock()
	d.badTopics[topicID] = time.Now().Add(d.cfg.badTopicTTL)
	d.mu.Unlock()
}

func (d *monitorDispatcher) deliverSendWithTopicFallback(ctx context.Context, topicID int, text string) (*botgotd.Message, bool, error) {
	d.mu.Lock()
	fallbackTop := d.fallbackTop
	d.mu.Unlock()

	var candidates []int
	addCand := func(id int) {
		for _, existing := range candidates {
			if existing == id {
				return
			}
		}
		if id != 0 && d.isTopicBad(id) {
			return
		}
		candidates = append(candidates, id)
	}
	addCand(topicID)
	if topicID != 0 && fallbackTop != 0 {
		addCand(fallbackTop)
	}
	addCand(0)

	var lastErr error
	for _, cand := range candidates {
		msg, err := d.execSend(ctx, cand, text)
		if err == nil {
			return msg, false, nil
		}
		lastErr = err
		if wait, isFlood := parseMonitorRetryAfter(err, d.cfg.maxWaitCap); isFlood {
			d.recordCooldown(wait)
			return nil, true, err
		}
		if isPermanentTopicError(err) {
			d.markTopicBad(cand)
			continue
		}
		return nil, false, err
	}
	return nil, false, lastErr
}

func (d *monitorDispatcher) deliverJob(ctx context.Context, item monitorJob) {
	text := truncateUTF8(item.text, 4096)
	_, isFlood, err := d.deliverSendWithTopicFallback(ctx, item.topicID, text)
	if err == nil {
		return
	}
	if isFlood {
		if item.allowRetry && item.retries < d.cfg.maxRetries {
			item.retries++
			item.priority = monitorPriorityHigh
			d.Enqueue(item)
		}
		return
	}
	log.Printf("⚠️ [DevMonitor] gagal kirim ke GROUP_DEV (topic=%d): %v", item.topicID, err)
}

func (d *monitorDispatcher) deliverDashboard(ctx context.Context, dash *dashboardEditJob) {
	text := truncateUTF8(dash.text, 4096)
	if dash.msgID > 0 {
		_, err := d.execEdit(ctx, dash.msgID, text)
		if err == nil || isMessageNotModifiedError(err) {
			return
		}
		if wait, isFlood := parseMonitorRetryAfter(err, d.cfg.maxWaitCap); isFlood {
			d.recordCooldown(wait)
			return
		}
		if !isMessageNotFoundError(err) {
			log.Printf("⚠️ [Telemetry] gagal edit dashboard (msg=%d): %v", dash.msgID, err)
			return
		}
	}

	msg, _, err := d.deliverSendWithTopicFallback(ctx, dash.topicID, text)
	if err == nil && msg != nil && msg.ID > 0 && dash.onMsgID != nil {
		dash.onMsgID(msg.ID)
	}
}

var retryAfterRe = regexp.MustCompile(`(?i)(?:retry_after["']?\s*[:=]\s*|retry after\s+|flood_wait_)(\d+)`)

func parseMonitorRetryAfter(err error, capDur time.Duration) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	if capDur <= 0 {
		capDur = 5 * time.Minute
	}
	if wait, ok := tgerr.AsFloodWait(err); ok {
		if wait < time.Second {
			wait = time.Second
		}
		if wait > capDur {
			wait = capDur
		}
		return wait, true
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	if !containsAny(low, "status 429", "too many requests", "flood_wait", "retry_after", "retry after") {
		return 0, false
	}
	wait := 15 * time.Second
	if m := retryAfterRe.FindStringSubmatch(msg); len(m) == 2 {
		if secs, convErr := strconv.Atoi(m[1]); convErr == nil && secs > 0 {
			wait = time.Duration(secs) * time.Second
		}
	}
	if wait < time.Second {
		wait = time.Second
	}
	if wait > capDur {
		wait = capDur
	}
	return wait, true
}

func containsAny(low string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(low, sub) {
			return true
		}
	}
	return false
}

func isPermanentTopicError(err error) bool {
	if err == nil {
		return false
	}
	if _, isFlood := parseMonitorRetryAfter(err, time.Minute); isFlood {
		return false
	}
	return containsAny(strings.ToLower(err.Error()),
		"message_thread_not_found",
		"message thread not found",
		"topic_closed",
		"topic closed",
		"topic_deleted",
		"topic deleted",
		"reply_message_id_invalid",
		"reply message not found",
	)
}

func isMessageNotModifiedError(err error) bool {
	if err == nil {
		return false
	}
	return containsAny(strings.ToLower(err.Error()), "message is not modified", "message_not_modified")
}

func isMessageNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return containsAny(strings.ToLower(err.Error()),
		"message to edit not found",
		"message_id_invalid",
		"message not found",
		"message can't be edited",
		"message cant be edited",
	)
}

func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	if cut > 3 {
		cut -= 3
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
