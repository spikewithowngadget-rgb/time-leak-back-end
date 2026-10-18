package handler

import (
	_ "embed"
	"net/http"
)

const swaggerUIHTML = `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <title>Timeleak Swagger</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({ url: "/swagger.json", dom_id: "#swagger-ui" });
  </script>
</body>
</html>`

//go:embed openapi.json
var swaggerSpec string

func (h *Handler) SwaggerJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerSpec))
}

func (h *Handler) SwaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerUIHTML))
}
