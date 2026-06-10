package main

import (
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"

	"strangler-fix-proxy/pkg/proxy"
)

func LoadConfig() *proxy.Config {
	routes, err := proxy.ParseRoutes(getEnv("NEW_SERVER_ROUTES", ""))
	if err != nil {
		log.Fatalf("Invalid NEW_SERVER_ROUTES: %v", err)
	}

	config := &proxy.Config{
		MainServerURL:         normalizeURL(getEnv("MAIN_SERVER_URL", "")),
		NewServerURL:          normalizeURL(getEnv("NEW_SERVER_URL", "")),
		SamplingRate:          getEnvFloat("SAMPLING_RATE", 1.0),
		DatabasePath:          getEnv("DATABASE_PATH", "./strangler_fig.db"),
		DatabaseMaxSizeMB:     getEnvInt("DATABASE_MAX_SIZE_MB", 1000),
		DatabaseRetentionDays: getEnvInt("DATABASE_RETENTION_DAYS", 7),
		Port:                  getEnv("PORT", "8080"),
		Routes:                routes,
	}

	return config
}

// normalizeURL ensures server URLs have an explicit scheme and port so they
// can be compared and parsed consistently (e.g. "example.com" becomes
// "http://example.com:80").
func normalizeURL(raw string) string {
	if raw == "" {
		return ""
	}

	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	if u.Port() == "" {
		switch u.Scheme {
		case "https":
			u.Host += ":443"
		default:
			u.Host += ":80"
		}
	}

	return u.String()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}

func getEnvSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
