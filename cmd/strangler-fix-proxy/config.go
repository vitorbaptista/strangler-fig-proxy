package main

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	MainServerURL         string
	NewServerURL          string
	SamplingRate          float64
	DatabasePath          string
	DatabaseMaxSizeMB     int
	DatabaseRetentionDays int
	Port                  string
	NewServerRoutes       []string
}

func LoadConfig() *Config {
	config := &Config{
		MainServerURL:         getEnv("MAIN_SERVER_URL", ""),
		NewServerURL:          getEnv("NEW_SERVER_URL", ""),
		SamplingRate:          getEnvFloat("SAMPLING_RATE", 1.0),
		DatabasePath:          getEnv("DATABASE_PATH", "./strangler_fig.db"),
		DatabaseMaxSizeMB:     getEnvInt("DATABASE_MAX_SIZE_MB", 1000),
		DatabaseRetentionDays: getEnvInt("DATABASE_RETENTION_DAYS", 7),
		Port:                  getEnv("PORT", "8080"),
		NewServerRoutes:       getEnvSlice("NEW_SERVER_ROUTES", []string{}),
	}

	return config
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

func (c *Config) ShouldRouteToNewServer(path string) bool {
	for _, route := range c.NewServerRoutes {
		if strings.HasPrefix(path, route) {
			return true
		}
	}
	return false
}
