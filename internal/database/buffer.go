package database

import (
	"sync"
	"time"
)

const (
	flushInterval = 30 * time.Second
	flushSize     = 100
)

// ShareEntry represents a share to be persisted.
type ShareEntry struct {
	Timestamp    int64
	MinerID      string
	Worker       string
	Difficulty   float64
	Accepted     bool
	RejectReason string
}

// Buffer batches share writes and flushes them periodically or when full.
type Buffer struct {
	db      *DB
	shares  []ShareEntry
	mu      sync.Mutex
	stop    chan struct{}
	stopped chan struct{}
}

// NewBuffer creates a write-behind buffer for the given database.
func NewBuffer(db *DB) *Buffer {
	b := &Buffer{
		db:      db,
		shares:  make([]ShareEntry, 0, flushSize),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go b.loop()
	return b
}

// AddShare queues a share for batch insertion.
func (b *Buffer) AddShare(entry ShareEntry) {
	b.mu.Lock()
	b.shares = append(b.shares, entry)
	needsFlush := len(b.shares) >= flushSize
	b.mu.Unlock()

	if needsFlush {
		go b.Flush()
	}
}

// Flush writes all buffered shares to the database.
func (b *Buffer) Flush() {
	b.mu.Lock()
	if len(b.shares) == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.shares
	b.shares = make([]ShareEntry, 0, flushSize)
	b.mu.Unlock()

	b.db.InsertShares(batch)
}

// Stop flushes remaining data and stops the background loop.
func (b *Buffer) Stop() {
	close(b.stop)
	<-b.stopped
	b.Flush() // Final flush
}

func (b *Buffer) loop() {
	defer close(b.stopped)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.stop:
			return
		case <-ticker.C:
			b.Flush()
		}
	}
}
