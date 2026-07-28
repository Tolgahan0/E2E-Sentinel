package httpserver

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// PostgresPinger adapts *pgxpool.Pool to the Pinger interface.
type PostgresPinger struct{ Pool *pgxpool.Pool }

// Ping checks the pool's connectivity.
func (p PostgresPinger) Ping(ctx context.Context) error { return p.Pool.Ping(ctx) }

// RedisPinger adapts *redis.Client to the Pinger interface.
type RedisPinger struct{ Client *redis.Client }

// Ping checks the client's connectivity.
func (p RedisPinger) Ping(ctx context.Context) error { return p.Client.Ping(ctx).Err() }
