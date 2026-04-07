package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Job struct {
	TransactionID int64 `json:"transaction_id"`
	Attempt       int   `json:"attempt"`
}

type RedisQueue struct {
	client *redis.Client
	name   string
}

func NewRedisQueue(addr, password string, db int, name string) *RedisQueue {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisQueue{client: client, name: name}
}

func (q *RedisQueue) Enqueue(ctx context.Context, job Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	if err := q.client.RPush(ctx, q.name, payload).Err(); err != nil {
		return fmt.Errorf("enqueue job: %w", err)
	}
	return nil
}

func (q *RedisQueue) Dequeue(ctx context.Context, timeout time.Duration) (Job, error) {
	result, err := q.client.BLPop(ctx, timeout, q.name).Result()
	if err != nil {
		return Job{}, err
	}
	if len(result) != 2 {
		return Job{}, fmt.Errorf("unexpected dequeue result length %d", len(result))
	}
	var job Job
	if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
		return Job{}, fmt.Errorf("unmarshal job: %w", err)
	}
	return job, nil
}

func (q *RedisQueue) Ping(ctx context.Context) error {
	return q.client.Ping(ctx).Err()
}

func (q *RedisQueue) Close() error {
	return q.client.Close()
}
