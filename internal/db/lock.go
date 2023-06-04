package db

import "context"

// Basis of inspiration: https://blogtitle.github.io/go-advanced-concurrency-patterns-part-3-channels/#read-write-mutexes

type rwMutex struct {
	writer  chan struct{}
	readers chan uint
}

func makeLock() rwMutex {
	return rwMutex{
		writer:  make(chan struct{}, 1),
		readers: make(chan uint, 1),
	}
}

func (m rwMutex) Lock() {
	// There's only room if no other writer or readers are holding the lock.
	m.writer <- struct{}{}
}

func (m rwMutex) Unlock() {
	// There is only an item to receive if another writer is holding the lock. (There could be an
	// item available due to readers holding the lock, but calling Unlock before RUnlock violates
	// the protocol for using the lock.)
	<-m.writer
}

func (m rwMutex) RLock() {
	var readers uint
	select {
	case m.writer <- struct{}{}:
		// We have no readers and no other writer.
	case readers = <-m.readers:
		// We have other readers.
	}
	readers++
	m.readers <- readers
}

func (m rwMutex) RUnlock() {
	readers := <-m.readers
	readers--
	if readers == 0 {
		// Allow any writers to acquire the lock again.
		<-m.writer
		return
	}
	// NB: We never send a nonpositive value to the readers channel.
	// NB: The writers channel still holds a value, blocking attempts to send a value.
	m.readers <- readers
}

func (m rwMutex) TryLockUntil(ctx context.Context) bool {
	select {
	// There's only room if no other writer or readers are holding the lock.
	case m.writer <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (m rwMutex) TryRLockUntil(ctx context.Context) bool {
	var readers uint
	select {
	case m.writer <- struct{}{}:
		// We have no readers and no other writer.
	case readers = <-m.readers:
		// We have other readers.
	case <-ctx.Done():
		return false
	}
	readers++
	m.readers <- readers
	return true
}
