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

	"github.com/tk/bangkok-commute-map/backend/internal/cache"
	"github.com/tk/bangkok-commute-map/backend/internal/commute"
	"github.com/tk/bangkok-commute-map/backend/internal/config"
	"github.com/tk/bangkok-commute-map/backend/internal/db"
	apihttp "github.com/tk/bangkok-commute-map/backend/internal/http"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	cacheClient := cache.NewFromURL(cfg.RedisURL, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	defer func() { _ = cacheClient.Close() }()
	if err := cacheClient.Ping(ctx); err != nil {
		log.Printf("warning: redis unavailable (%v); continuing with graceful fallback", err)
	}

	repo := commute.NewRepository(pool)
	graph, err := repo.LoadTransitGraph(ctx)
	if err != nil {
		log.Fatalf("load transit graph from postgis: %v", err)
	}
	log.Printf("loaded in-memory transit graph with %d stations", len(graph.AllStations()))

	commuteSvc := commute.NewService(repo, graph, cacheClient)
	router := apihttp.NewRouter(pool, cacheClient, commuteSvc, cfg.CORSAllowOrigin)

	srv := &http.Server{
		Addr:              ":" + cfg.APIPort,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("Bangkok Commute Map API listening on :%s", cfg.APIPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down API server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}
	log.Println("server exited cleanly")
}
