package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"highload_final/internal/model"
)

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(addr, password string, db int) *RedisStore {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisStore{client: client}
}

func (s *RedisStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *RedisStore) Close() error {
	return s.client.Close()
}

func (s *RedisStore) StoreMetric(ctx context.Context, m model.Metric) error {
	payload, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal metric: %w", err)
	}

	pipe := s.client.Pipeline()
	pipe.Set(ctx, "metrics:latest", payload, time.Hour)
	pipe.LPush(ctx, "metrics:recent", payload)
	pipe.LTrim(ctx, "metrics:recent", 0, 999)
	if m.DeviceID != "" {
		pipe.Set(ctx, "metrics:latest:"+m.DeviceID, payload, time.Hour)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis exec: %w", err)
	}

	return nil
}

func (s *RedisStore) LoadLatest(ctx context.Context, deviceID string) (*model.Metric, error) {
	key := "metrics:latest"
	if deviceID != "" {
		key = "metrics:latest:" + deviceID
	}

	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("redis get: %w", err)
	}

	var m model.Metric
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshal metric: %w", err)
	}

	return &m, nil
}