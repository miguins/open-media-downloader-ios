// Package config loads and validates application configuration.
package config

import (
	"errors"
	"net"
	"strconv"
	"strings"
)

const defaultHTTPAddr = ":8080"

// Config contains validated application configuration.
type Config struct {
	HTTPAddr string
}

// Load reads and validates application configuration through lookup.
func Load(lookup func(string) (string, bool)) (Config, error) {
	value, present := lookup("OMDI_HTTP_ADDR")
	if !present {
		value = defaultHTTPAddr
	}
	if value == "" || value != strings.TrimSpace(value) {
		return Config{}, errors.New("OMDI_HTTP_ADDR must not be empty or contain surrounding whitespace")
	}

	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return Config{}, errors.New("OMDI_HTTP_ADDR must use host:port syntax")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return Config{}, errors.New("OMDI_HTTP_ADDR must contain a port from 1 to 65535")
	}

	return Config{HTTPAddr: value}, nil
}
