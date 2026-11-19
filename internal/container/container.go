package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/eternal-despair/template/internal/config"
	pushGateway "github.com/eternal-despair/template/internal/gateway/push"
	api "github.com/eternal-despair/template/internal/generated"
	apphttp "github.com/eternal-despair/template/internal/http"
	"github.com/eternal-despair/template/internal/http/middleware"
	createHandler "github.com/eternal-despair/template/internal/http/trip/create"
	finishHandler "github.com/eternal-despair/template/internal/http/trip/finish"
	getHandler "github.com/eternal-despair/template/internal/http/trip/get"
	createPosHandler "github.com/eternal-despair/template/internal/http/trip/position/create"
	listPosHandler "github.com/eternal-despair/template/internal/http/trip/position/list"
	"github.com/eternal-despair/template/internal/infra/postgres"
	"github.com/eternal-despair/template/internal/infra/postgres/transaction"
	"github.com/eternal-despair/template/internal/infra/telemetry"
	tripRepo "github.com/eternal-despair/template/internal/repository/trip"
	createUC "github.com/eternal-despair/template/internal/usecase/trip/create"
	finishUC "github.com/eternal-despair/template/internal/usecase/trip/finish"
	getUC "github.com/eternal-despair/template/internal/usecase/trip/get"
	createPosUC "github.com/eternal-despair/template/internal/usecase/trip/position/create"
	listPosUC "github.com/eternal-despair/template/internal/usecase/trip/position/list"
	positionsWorker "github.com/eternal-despair/template/internal/worker/positions"
)

type Container struct {
	cfg        *config.Config
	logger     *slog.Logger
	pool       *pgxpool.Pool
	telemetry  *telemetry.Telemetry
	httpServer *nethttp.Server
	workerPool *positionsWorker.Pool
}

func New(ctx context.Context, cfg *config.Config) (*Container, error) {
	tel, err := telemetry.Setup(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("setup telemetry: %w", err)
	}

	logger := telemetry.NewLogger(os.Stdout, cfg.LogLevel, cfg.OtelServiceName)

	pool, err := postgres.NewPool(ctx, cfg)
	if err != nil {
		_ = tel.Shutdown(ctx)
		return nil, fmt.Errorf("init postgres pool: %w", err)
	}

	txMgr := transaction.New(pool)
	repo := tripRepo.New(pool)

	gateway := pushGateway.NewClient(cfg.PushHTTPURL, cfg.PushRequestTimeout, tel.Tracer)

	poolCfg := positionsWorker.Config{
		CheckInterval: cfg.PositionCheckInterval,
		StaleAfter:    cfg.PositionStaleAfter,
		Workers:       cfg.PositionWorkers,
		QueueSize:     cfg.PositionQueueSize,
		PushTimeout:   cfg.PushRequestTimeout,
	}
	workerPool := positionsWorker.NewPool(poolCfg, repo, gateway, tel.Metrics, logger, tel.Tracer, nil)

	createUsecase := createUC.New(repo, txMgr, logger, tel.Metrics)
	getUsecase := getUC.New(repo)
	finishUsecase := finishUC.New(repo, txMgr, logger, tel.Metrics)
	createPosUsecase := createPosUC.New(repo, txMgr, workerPool, tel.Metrics)
	listPosUsecase := listPosUC.New(repo)

	createH := createHandler.New(createUsecase)
	getH := getHandler.New(getUsecase)
	finishH := finishHandler.New(finishUsecase)
	createPosH := createPosHandler.New(createPosUsecase)
	listPosH := listPosHandler.New(listPosUsecase)
	apiServer := apphttp.NewServer(createH, getH, finishH, createPosH, listPosH, pool)

	r := chi.NewRouter()
	r.Use(middleware.Recover(logger))
	r.Use(middleware.Tracing(tel.Tracer))
	r.Use(middleware.Logging(logger))
	r.Use(middleware.Metrics(tel.Metrics))

	api.HandlerFromMux(apiServer, r)

	httpServer := &nethttp.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return &Container{
		cfg:        cfg,
		logger:     logger,
		pool:       pool,
		telemetry:  tel,
		httpServer: httpServer,
		workerPool: workerPool,
	}, nil
}

func (c *Container) Run() error {
	c.logger.Info("trip-service starting", slog.String("addr", c.cfg.HTTPAddr))
	c.workerPool.Start()

	err := c.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
		return err
	}
	return nil
}

func (c *Container) Shutdown(ctx context.Context) error {
	c.logger.Info("trip-service shutting down")

	if err := c.httpServer.Shutdown(ctx); err != nil {
		c.logger.Error("error shutting down http server", slog.Any("error", err))
	}

	c.workerPool.StopScanner()

	if err := c.workerPool.Shutdown(ctx); err != nil {
		c.logger.Error("error shutting down worker pool", slog.Any("error", err))
		return err
	}

	return nil
}

func (c *Container) Close() {
	if c.pool != nil {
		c.pool.Close()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c.telemetry != nil {
		_ = c.telemetry.Shutdown(shutdownCtx)
	}
	c.logger.Info("trip-service stopped cleanly")
}
