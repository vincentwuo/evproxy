package concurrent

import (
	"sync/atomic"
)

type AtomicLimiter struct {
	max    int64
	count  int64
	enable int64
}

func NewAtomicLimiter(maxCocurrent int64) (*AtomicLimiter, error) {
	var enable int64 = 1
	if maxCocurrent <= 0 {
		maxCocurrent = 0
		enable = 0
	}

	return &AtomicLimiter{
		max:    maxCocurrent,
		enable: enable,
		count:  0,
	}, nil
}

func (b *AtomicLimiter) Acquire() (bool, int64) {
	nowN := atomic.LoadInt64(&b.count)
	if atomic.LoadInt64(&b.enable) != 1 {
		return true, nowN
	}

	if nowN >= b.max {
		return false, nowN
	} else {
		atomic.AddInt64(&b.count, 1)
		return true, nowN
	}

}

func (b *AtomicLimiter) Reset(limit int64) {
	if limit <= 0 {
		b.enable = 0
		limit = 0
	}
	if limit > 0 {
		b.enable = 1
	}
	atomic.StoreInt64(&b.max, limit)
}

func (b *AtomicLimiter) Release() {
	atomic.AddInt64(&b.count, -1)
}

func (b *AtomicLimiter) Disable() {
	atomic.StoreInt64(&b.enable, 0)
}

func (b *AtomicLimiter) Count() int64 {
	return atomic.LoadInt64(&b.count)
}
