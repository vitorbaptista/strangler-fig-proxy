package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"strangler-fix-proxy/pkg/proxy"
)

func main() {
	config := LoadConfig()

	if config.MainServerURL == "" {
		log.Fatal("MAIN_SERVER_URL is required")
	}
	if config.NewServerURL == "" {
		log.Fatal("NEW_SERVER_URL is required")
	}

	database, err := proxy.InitDatabase(config.DatabasePath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	proxyHandler := proxy.NewProxyHandler(config, database)

	server := &http.Server{
		Addr:    ":" + config.Port,
		Handler: proxyHandler,
	}

	go func() {
		log.Printf("Starting strangler fig proxy on port %s", config.Port)
		log.Printf("Main server: %s", config.MainServerURL)
		log.Printf("New server: %s", config.NewServerURL)
		log.Printf("Database: %s", config.DatabasePath)
		log.Printf("Sampling rate: %.2f", config.SamplingRate)
		log.Printf("Dashboard: http://localhost:%s/__strangler_fig", config.Port)

		if routes := config.GetRoutes(); len(routes) > 0 {
			for _, route := range routes {
				log.Printf("Route: %s -> new server (%.0f%% of traffic)", route.Prefix, route.Percentage)
			}
		} else {
			log.Printf("No new server routes configured - all traffic goes to main server")
		}

		log.Printf("Proxy ready to handle requests")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
