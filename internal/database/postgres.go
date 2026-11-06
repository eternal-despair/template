package database

import (
	"context"
	"fmt"

	"github.com/eternal-despair/template/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool инициализирует пул соединений к PostgreSQL с настройками из конфига
func NewPostgresPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}

	// Настраиваем параметры пула из нашего конфига
	poolConfig.MaxConns = cfg.DatabaseMaxConns
	poolConfig.MinConns = cfg.DatabaseMinConns
	poolConfig.MaxConnLifetime = cfg.DatabaseMaxConnLifetime

	// Контекст с таймаутом на подключение
	connectCtx, cancel := context.WithTimeout(ctx, cfg.DatabaseConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	// Требование задания: на старте обязательный Ping. Если база недоступна — сервис не стартует!
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
