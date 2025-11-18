package ack

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	queueBucket = []byte("ack-queue")
	metaBucket  = []byte("ack-meta")

	ErrQueueEmpty = errors.New("ack queue empty")
	ErrQueueFull  = errors.New("ack queue full")
)

// Queue persists ACK payloads for retry.
type Queue interface {
	Enqueue(ctx context.Context, payload Payload) (uint64, error)
	Peek(ctx context.Context) (uint64, Payload, error)
	Delete(ctx context.Context, id uint64) error
	Len(ctx context.Context) (int, error)
	Notify() <-chan struct{}
	Close() error
}

// BoltQueue implements Queue using bbolt.
type BoltQueue struct {
	db       *bolt.DB
	maxSize  int
	enqueueC chan struct{}
}

// QueueOptions configure BoltQueue.
type QueueOptions struct {
	Path    string
	MaxSize int
}

// OpenQueue opens/creates queue at path.
func OpenQueue(opts QueueOptions) (*BoltQueue, error) {
	if opts.Path == "" {
		return nil, errors.New("ack queue path required")
	}
	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o755); err != nil {
		return nil, fmt.Errorf("ack queue: mkdir: %w", err)
	}
	db, err := bolt.Open(opts.Path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("ack queue: open: %w", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(queueBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(metaBucket); err != nil {
			return err
		}
		return nil
	}); err != nil {
		db.Close()
		return nil, fmt.Errorf("ack queue: init buckets: %w", err)
	}
	max := opts.MaxSize
	if max <= 0 {
		max = 10000
	}
	return &BoltQueue{db: db, maxSize: max, enqueueC: make(chan struct{}, 1)}, nil
}

func (q *BoltQueue) Enqueue(ctx context.Context, payload Payload) (uint64, error) {
	var id uint64
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("ack queue: marshal payload: %w", err)
	}
	err = q.db.Update(func(tx *bolt.Tx) error {
		queue := tx.Bucket(queueBucket)
		meta := tx.Bucket(metaBucket)
		if queue == nil || meta == nil {
			return errors.New("ack queue: buckets missing")
		}
		if queue.Stats().KeyN >= q.maxSize {
			return ErrQueueFull
		}
		seq, err := meta.NextSequence()
		if err != nil {
			return err
		}
		timestamp := time.Now().UTC().UnixNano()
		key := make([]byte, 16)
		binary.BigEndian.PutUint64(key[:8], seq)
		binary.BigEndian.PutUint64(key[8:], uint64(timestamp))
		if err := queue.Put(key, data); err != nil {
			return err
		}
		id = seq
		return nil
	})
	if err != nil {
		return 0, err
	}
	select {
	case q.enqueueC <- struct{}{}:
	default:
	}
	return id, nil
}

func (q *BoltQueue) Peek(ctx context.Context) (uint64, Payload, error) {
	var (
		payload Payload
		id      uint64
	)
	err := q.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(queueBucket)
		if bucket == nil {
			return errors.New("ack queue: bucket missing")
		}
		cursor := bucket.Cursor()
		k, v := cursor.First()
		if k == nil {
			return ErrQueueEmpty
		}
		id = binary.BigEndian.Uint64(k[:8])
		return json.Unmarshal(v, &payload)
	})
	if errors.Is(err, ErrQueueEmpty) {
		return 0, Payload{}, ErrQueueEmpty
	}
	return id, payload, err
}

func (q *BoltQueue) Delete(ctx context.Context, id uint64) error {
	return q.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(queueBucket)
		if bucket == nil {
			return errors.New("ack queue: bucket missing")
		}
		cursor := bucket.Cursor()
		k, _ := cursor.First()
		for k != nil {
			curID := binary.BigEndian.Uint64(k[:8])
			if curID == id {
				return bucket.Delete(k)
			}
			k, _ = cursor.Next()
		}
		return fmt.Errorf("ack queue: id %d not found", id)
	})
}

func (q *BoltQueue) Len(ctx context.Context) (int, error) {
	var count int
	err := q.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(queueBucket)
		if bucket == nil {
			return errors.New("ack queue: bucket missing")
		}
		count = bucket.Stats().KeyN
		return nil
	})
	return count, err
}

func (q *BoltQueue) Close() error {
	return q.db.Close()
}

func (q *BoltQueue) Notify() <-chan struct{} {
	return q.enqueueC
}
