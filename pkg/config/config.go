package config

import "time"

type Config struct {
	LogLevel     int
	ScanInterval time.Duration
	TenantID     string
	CDIEndpoint  string
}
