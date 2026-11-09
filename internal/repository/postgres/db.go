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
