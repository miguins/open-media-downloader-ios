package api

import (
	"io"
	"net/http"
)

const healthBody = "{\"status\":\"ok\"}\n"

func healthz(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(response, healthBody); err != nil {
		return
	}
}
