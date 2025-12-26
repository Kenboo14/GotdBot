package utils

import "time"

type Backoff struct {
	attempt int
	max     int
}

func NewBackoff() *Backoff {
	return &Backoff{attempt: 0, max: 5}
}

func (b *Backoff) Attempt() bool {
	if b.attempt >= b.max {
		return false
	}
	sleep := time.Duration(b.attempt*b.attempt) * time.Second
	time.Sleep(sleep)
	b.attempt++
	return true
}
