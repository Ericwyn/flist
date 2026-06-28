package util

import "sync"

// PathLocker 提供进程内的「按 key（通常是路径）互斥」能力。
//
// flist 是单进程服务，并发来自 goroutine 而非跨进程，因此用进程内 keyed mutex 即可
// 把对同一目标的写操作串行化（见 0.backend-design.md §9.5），无需 OS 级 flock。
//
// 实现要点：每个 key 维护一个 sync.Mutex 与引用计数，最后一个持有者 Unlock 时
// 从 map 移除该条目，避免长期运行后 map 无限增长。
type PathLocker struct {
	mu    sync.Mutex
	locks map[string]*pathLockEntry
}

type pathLockEntry struct {
	mu  sync.Mutex
	ref int // 等待 + 持有该锁的 goroutine 数，归零即可回收
}

// NewPathLocker 构造一个空的路径锁管理器。
func NewPathLocker() *PathLocker {
	return &PathLocker{locks: make(map[string]*pathLockEntry)}
}

// Lock 获取 key 对应的互斥锁，阻塞直到取得。必须与一次 Unlock 配对。
func (l *PathLocker) Lock(key string) {
	l.mu.Lock()
	e, ok := l.locks[key]
	if !ok {
		e = &pathLockEntry{}
		l.locks[key] = e
	}
	e.ref++
	l.mu.Unlock()

	e.mu.Lock()
}

// Unlock 释放 key 对应的互斥锁；当无其他等待者时回收该条目。
func (l *PathLocker) Unlock(key string) {
	l.mu.Lock()
	e, ok := l.locks[key]
	if !ok {
		l.mu.Unlock()
		return // 未加锁的 key，宽松处理（不 panic）
	}
	e.ref--
	if e.ref == 0 {
		delete(l.locks, key)
	}
	l.mu.Unlock()

	e.mu.Unlock()
}
