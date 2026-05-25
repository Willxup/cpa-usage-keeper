package poller

import "time"

type RedisIngestBackoff struct {
	// initial 是第一次连续失败后的等待时间，当前设计为 1s。
	initial time.Duration
	// max 是退避上限，避免故障长期存在时等待无限增长。
	max time.Duration
	// next 保存下一次失败应该返回的等待时间。
	next time.Duration
}

func NewRedisIngestBackoff(initial, max time.Duration) *RedisIngestBackoff {
	// 构造时把 next 放在 initial，确保第一次失败立即使用初始退避。
	return &RedisIngestBackoff{initial: initial, max: max, next: initial}
}

func (b *RedisIngestBackoff) NextDelay() time.Duration {
	if b == nil {
		// nil backoff 不能阻塞状态机，使用安全默认 1s。
		return time.Second
	}
	// 当前调用返回 next 中保存的等待时间。
	delay := b.next
	if delay <= 0 {
		// next 非法时回退到 initial。
		delay = b.initial
	}
	if delay <= 0 {
		// initial 也非法时兜底 1s，避免返回 0 导致忙循环。
		delay = time.Second
	}
	// 下一次失败等待翻倍。
	next := delay * 2
	if b.max > 0 && next > b.max {
		// 超过上限后固定在 max。
		next = b.max
	}
	// 保存下一次失败使用的等待时间。
	b.next = next
	// 返回本次失败应该等待的时间。
	return delay
}

func (b *RedisIngestBackoff) Reset() {
	if b == nil {
		// nil backoff 没有状态可重置。
		return
	}
	// 任一拉取链路成功后，下一次失败重新从 initial 开始。
	b.next = b.initial
}
