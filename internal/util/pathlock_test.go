package util

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPathLocker_SameKeySerializes 验证同一 key 的临界区被串行化：
// 并发增量在持锁内进行，最终计数应精确等于 goroutine 数（无竞态丢失）。
func TestPathLocker_SameKeySerializes(t *testing.T) {
	l := NewPathLocker()
	const n = 100
	counter := 0
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			l.Lock("same")
			// 故意非原子读改写，靠锁保证正确性。
			c := counter
			c++
			counter = c
			l.Unlock("same")
		}()
	}
	wg.Wait()
	if counter != n {
		t.Errorf("expected counter %d under serialized lock, got %d", n, counter)
	}
}

// TestPathLocker_SameKeyMutualExclusion 验证同 key 任意时刻至多一个持有者。
func TestPathLocker_SameKeyMutualExclusion(t *testing.T) {
	l := NewPathLocker()
	var inside int32
	var maxConcurrent int32
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			l.Lock("k")
			cur := atomic.AddInt32(&inside, 1)
			if cur > atomic.LoadInt32(&maxConcurrent) {
				atomic.StoreInt32(&maxConcurrent, cur)
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&inside, -1)
			l.Unlock("k")
		}()
	}
	wg.Wait()
	if maxConcurrent != 1 {
		t.Errorf("same key should allow at most 1 concurrent holder, saw %d", maxConcurrent)
	}
}

// TestPathLocker_DifferentKeysParallel 验证不同 key 可并行（不互相阻塞）。
func TestPathLocker_DifferentKeysParallel(t *testing.T) {
	l := NewPathLocker()
	l.Lock("a")
	defer l.Unlock("a")

	done := make(chan struct{})
	go func() {
		l.Lock("b") // 不同 key，应立即取得而非阻塞在 a 上。
		l.Unlock("b")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("different key lock should not block on another key")
	}
}

// TestPathLocker_ReusableAfterUnlock 验证 Unlock 后同 key 可再次 Lock，且条目被回收。
func TestPathLocker_ReusableAfterUnlock(t *testing.T) {
	l := NewPathLocker()
	l.Lock("x")
	l.Unlock("x")
	l.Lock("x") // 不应死锁。
	l.Unlock("x")

	l.mu.Lock()
	remaining := len(l.locks)
	l.mu.Unlock()
	if remaining != 0 {
		t.Errorf("lock entry should be reclaimed after unlock, got %d entries", remaining)
	}
}

// TestPathLocker_UnlockUnknownKeyNoPanic 验证未加锁的 key Unlock 不 panic。
func TestPathLocker_UnlockUnknownKeyNoPanic(t *testing.T) {
	l := NewPathLocker()
	l.Unlock("never-locked") // 不应 panic。
}
