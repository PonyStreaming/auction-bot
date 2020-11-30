package api

import (
	"crypto/subtle"
	"net/http"
)

type authedHandler struct {
	password string
	handler  http.Handler
}

func (ah *authedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("password")), []byte(ah.password)) != 1 {
		http.Error(w, "Unauthorized.", http.StatusUnauthorized)
		return
	}

	ah.handler.ServeHTTP(w, r)
}

func basicAuth(handler http.Handler, password string) http.Handler {
	if password == "" {
		return handler
	}
	return &authedHandler{
		password: password,
		handler:  handler,
	}
}
