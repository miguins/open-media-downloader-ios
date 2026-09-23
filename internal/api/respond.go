package api

import (
	"encoding/json"
	"io"
	"net/http"
)

func writeJSON(response http.ResponseWriter, status int, value any) {
	body, _ := json.Marshal(value) // Response types contain only strings, numbers, and times, which always encode.
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_, _ = response.Write(append(body, '\n'))
}

// writeError writes a fixed error code. Codes are constants, never derived from input or internal errors.
func writeError(response http.ResponseWriter, status int, code string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_, _ = io.WriteString(response, `{"error":"`+code+`"}`+"\n")
}
