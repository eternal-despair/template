package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eternal-despair/template/internal/config"
	"github.com/eternal-despair/template/internal/database"
	api "github.com/eternal-despair/template/internal/generated"
	"github.com/eternal-despair/template/internal/handler"
	"github.com/eternal-despair/template/internal/repository"
	"github.com/eternal-despair/template/internal/service"
	"github.com/eternal-despair/template/internal/txmanager"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	// 1. Загрузка конфигурации
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	ctx := context.Background()

	// 2. Инициализация пула БД (с пингом на старте)
	pool, err := database.NewPostgresPool(ctx, cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	// 3. Сборка зависимостей (Composition Root)
	txMgr := txmanager.New(pool)
	tripRepo := repository.NewTripRepository(pool)
	tripSvc := service.NewTripService(tripRepo, txMgr)
	tripHandler := handler.NewHandler(tripSvc, pool)

	// 4. Настройка роутера Chi и регистрация сгенерированного API
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	api.HandlerFromMux(tripHandler, r)

	// 5. Конфигурация HTTP-сервера с обязательными таймаутами
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 6. Запуск сервера в отдельной горутине
	serverErrCh := make(chan error, 1)
	go func() {
		log.Printf("trip-service started on %s", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	// 7. Graceful Shutdown по SIGINT / SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrCh:
		log.Fatalf("server failed: %v", err)
	case sig := <-sigCh:
		log.Printf("received signal %v, initiating graceful shutdown (budget: %v)...", sig, cfg.ShutdownTimeout)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}

	log.Println("trip-service stopped cleanly")
}
