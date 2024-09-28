package http

import (
	"bytes"
	"encoding/json"
	"github.com/mehmetsafabenli/cbomdekont/pkg/version"
	"go.uber.org/zap"
	"net/http"
)

func versionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-API-Version", version.VERSION)
		r.Header.Set("X-API-Revision", version.REVISION)

		next.ServeHTTP(w, r)
	})
}

func (s *Server) JSONResponse(w http.ResponseWriter, r *http.Request, result interface{}) {
	body, err := json.Marshal(result)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		s.logger.Error("JSON marshal failed", zap.Error(err))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	w.Write(prettyJSON(body))
}

func prettyJSON(b []byte) []byte {
	var out bytes.Buffer
	json.Indent(&out, b, "", "  ")
	return out.Bytes()
}
