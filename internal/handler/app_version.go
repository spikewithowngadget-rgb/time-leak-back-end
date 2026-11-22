package handler

import "net/http"

type appVersionResponse struct {
	Version     string `json:"version"`
	Changelog   string `json:"changelog"`
	ForceUpdate bool   `json:"force_update"`
}

func (h *Handler) AppVersion(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, appVersionResponse{
		Version:   "1.2.3",
		Changelog: "Добавлен повтор «Ежеквартально»",
	})
}
