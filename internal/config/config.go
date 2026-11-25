// Package config loads runtime settings from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config is everything the app needs to run, resolved at startup.
type Config struct {
	ClientID     string // Oura OAuth application client ID
	ClientSecret string // Oura OAuth application client secret
	TokenPath    string // path to the OAuth token file (access + refresh token)
	DBPath       string // path to database
	Days         int    // number of days of historical data to pull
}

// Load reads configuration from environment variables, applying defaults.
func Load() (*Config, error) {
	c := &Config{
		ClientID:     os.Getenv("OURA_CLIENT_ID"),     // from .env
		ClientSecret: os.Getenv("OURA_CLIENT_SECRET"), // from .env
		TokenPath:    "oura-token.json",               // default to relative path of token file
		DBPath:       DBPath(),                        // WH_DB_PATH or the default
		Days:         30,                              // default to 30 days
	}

	// Both are needed even with a valid token file: refreshing a token
	// authenticates the application, not just the user.
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, fmt.Errorf("OURA_CLIENT_ID and OURA_CLIENT_SECRET must be set (see .env.example)")
	}

	if v := os.Getenv("WH_TOKEN_PATH"); v != "" {
		c.TokenPath = v
	}

	if v := os.Getenv("WH_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("WH_DAYS: %w", err)
		}
		c.Days = n
	}
	return c, nil
}

// DBPath is the database location: WH_DB_PATH, or wolfe-health.db.
//
// Separate from Load because reading the database (charting) needs no Oura
// credentials, and should not fail for want of them.
func DBPath() string {
	if v := os.Getenv("WH_DB_PATH"); v != "" {
		return v
	}
	return "wolfe-health.db"
}
