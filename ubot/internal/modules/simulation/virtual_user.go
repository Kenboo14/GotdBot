package simulation

import (
	"context"
	"sync"
	"time"
)

type UserProfile string

const (
	ProfileLow  UserProfile = "low"
	ProfileMed  UserProfile = "medium"
	ProfileHigh UserProfile = "high"
)

type UserLifecycleState string

const (
	StateVirtualActive       UserLifecycleState = "ACTIVE"
	StateVirtualDisconnected UserLifecycleState = "DISCONNECTED"
	StateVirtualStopped      UserLifecycleState = "STOPPED"
)

type VirtualUser struct {
	mu        sync.RWMutex
	ID        int64
	Profile   UserProfile
	State     UserLifecycleState
	Interval  time.Duration
	Ctx       context.Context
	Cancel    context.CancelFunc
	Generated int64
	Processed int64
	Failed    int64
	StartTime time.Time
	StopTime  time.Time
}

func NewVirtualUser(id int64, profile UserProfile, parentCtx context.Context) *VirtualUser {
	ctx, cancel := context.WithCancel(parentCtx)

	var interval time.Duration
	switch profile {
	case ProfileHigh:
		interval = 4 * time.Second
	case ProfileMed:
		interval = 12 * time.Second
	default:
		interval = 60 * time.Second
	}

	return &VirtualUser{
		ID:        id,
		Profile:   profile,
		State:     StateVirtualActive,
		Interval:  interval,
		Ctx:       ctx,
		Cancel:    cancel,
		StartTime: time.Now(),
	}
}

func (u *VirtualUser) GetState() UserLifecycleState {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.State
}

func (u *VirtualUser) SetState(st UserLifecycleState) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.State = st
}

func (u *VirtualUser) Stop() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.State != StateVirtualStopped {
		u.State = StateVirtualStopped
		u.StopTime = time.Now()
		if u.Cancel != nil {
			u.Cancel()
		}
	}
}

func (u *VirtualUser) IncGenerated() {
	u.mu.Lock()
	u.Generated++
	u.mu.Unlock()
}

func (u *VirtualUser) IncProcessed() {
	u.mu.Lock()
	u.Processed++
	u.mu.Unlock()
}

func (u *VirtualUser) IncFailed() {
	u.mu.Lock()
	u.Failed++
	u.mu.Unlock()
}
