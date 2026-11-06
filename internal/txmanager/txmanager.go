package txmanager

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// txKey — уникальный приватный ключ для context.Context
type txKey struct{}

// Executor — общий интерфейс исполнителя запросов (реализуется и pgxpool.Pool, и pgx.Tx)
type Executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxManager управляет жизненным циклом транзакций через context
type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type txManager struct {
	pool *pgxpool.Pool
}

// New создает экземпляр TxManager
func New(pool *pgxpool.Pool) TxManager {
	return &txManager{pool: pool}
}

// Do выполняет переданную функцию fn внутри транзакции
func (m *txManager) Do(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	// 1. Вложенный вызов: если транзакция уже открыта в контексте, не открываем вторую
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	// 2. Открываем транзакцию с уровнем изоляции Read Committed
	txOpts := pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	}

	tx, err := m.pool.BeginTx(ctx, txOpts)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// 3. Rollback при панике или ошибке
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p) // пробрасываем панику наружу по требованиям задания
		} else if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// 4. Обогащаем контекст транзакцией
	ctxWithTx := context.WithValue(ctx, txKey{}, tx)

	// 5. Вызываем бизнес-функцию
	if err = fn(ctxWithTx); err != nil {
		return err
	}

	// 6. Фиксируем транзакцию
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// GetExecutor извлекает транзакцию из контекста. Если её нет, возвращает пул соединений
func GetExecutor(ctx context.Context, pool *pgxpool.Pool) Executor {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}
