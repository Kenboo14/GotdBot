package simulation

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/gotd/td/tg"
)

type FakeTransport struct {
	rnd *rand.Rand
}

func NewFakeTransport() *FakeTransport {
	return &FakeTransport{
		rnd: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (f *FakeTransport) GenerateUpdate(virtualID int64, cmdType string) *tg.Updates {
	msgID := int(f.rnd.Int31n(1000000) + 1)
	date := int(time.Now().Unix())

	var text string
	switch cmdType {
	case "ping":
		text = "!ping"
	case "help":
		text = "!help"
	case "status":
		text = "!statuslog"
	case "burst":
		text = fmt.Sprintf("!ping burst-%d", msgID)
	default:
		text = fmt.Sprintf("Synthetic message #%d from virtual user %d", msgID, virtualID)
	}

	msg := &tg.Message{
		ID:      msgID,
		Out:     true,
		Date:    date,
		Message: text,
		FromID:  &tg.PeerUser{UserID: virtualID},
		PeerID:  &tg.PeerUser{UserID: virtualID},
	}

	return &tg.Updates{
		Updates: []tg.UpdateClass{
			&tg.UpdateNewMessage{
				Message:  msg,
				Pts:      msgID,
				PtsCount: 1,
			},
		},
		Users: []tg.UserClass{
			&tg.User{
				ID:         virtualID,
				FirstName:  fmt.Sprintf("Virtual_%d", virtualID),
				AccessHash: virtualID * 1000,
			},
		},
	}
}
