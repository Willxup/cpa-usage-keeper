package service

import (
	"sync"

	"cpa-usage-keeper/internal/entities"
)

// UsageEventsNotifier 只保存最高已提交 ID 和每个订阅者一个可合并唤醒，不保存事件正文。
type UsageEventsNotifier struct {
	mu          sync.Mutex
	high        int64
	subscribers map[chan struct{}]struct{}
}

func NewUsageEventsNotifier() *UsageEventsNotifier {
	return &UsageEventsNotifier{subscribers: make(map[chan struct{}]struct{})}
}

func (n *UsageEventsNotifier) NotifyUsageEventsCommitted(events []entities.UsageEvent) {
	var high int64
	for _, event := range events {
		if event.ID > high {
			high = event.ID
		}
	}
	if high == 0 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if high <= n.high {
		return
	}
	n.high = high
	for ch := range n.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (n *UsageEventsNotifier) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	n.subscribers[ch] = struct{}{}
	n.mu.Unlock()
	return ch, func() {
		n.mu.Lock()
		delete(n.subscribers, ch)
		n.mu.Unlock()
	}
}
