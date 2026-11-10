// Package postgres реализует репозитории поверх пула коннектора инфраструктуры
// (github.com/arrowwhi/infrastructure/connections/postgres). Пул сам выполняет запрос
// в транзакции из контекста, поэтому одни и те же методы работают внутри InTx и вне её.
package postgres

import (
	"context"
	"fmt"

	infrapg "github.com/arrowwhi/infrastructure/connections/postgres"
)

const serviceName = "poker-bank"

// NewPool поднимает инструментированный пул (спаны, метрики, лог медленных запросов)
// и проверяет соединение: падать на старте понятнее, чем на первом обновлении.
func NewPool(ctx context.Context, dsn string) (*infrapg.Pool, error) {
	cfg := infrapg.DefaultConfig()
	cfg.DSN = dsn
	cfg.ApplicationName = serviceName
	cfg.TracingEnabled = true
	cfg.MetricsEnabled = false // иначе на каждый запрос без WithQueryName — предупреждение в логе
	cfg.SlowQueryLog = true

	pool, err := infrapg.New(ctx, cfg, infrapg.WithServiceName(serviceName))
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	return pool, nil
}

// TxManager запускает сервисные операции в транзакции пула (interfaces.TxManager).
type TxManager struct {
	pool *infrapg.Pool
}

// NewTxManager wraps the pool as interfaces.TxManager.
func NewTxManager(pool *infrapg.Pool) *TxManager { return &TxManager{pool: pool} }

// InTx runs fn in a transaction (a savepoint if ctx already carries one).
func (m *TxManager) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.pool.InTx(ctx, fn)
}
