package handler

import (
	"encoding/json"
	"net/http"
)

func (h *Handler) AppleAppSiteAssociation(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Sharing.AppleAppID == "" {
		writeErrorJSON(w, http.StatusServiceUnavailable, "Apple app links are not configured")
		return
	}
	writeAppLinksJSON(w, map[string]any{
		"applinks": map[string]any{
			"apps": []string{},
			"details": []map[string]any{{
				"appID": h.cfg.Sharing.AppleAppID,
				"paths": []string{"/share/*"},
			}},
		},
	})
}

func (h *Handler) AndroidAssetLinks(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Sharing.AndroidPackageName == "" || len(h.cfg.Sharing.AndroidSHA256CertFingerprints) == 0 {
		writeErrorJSON(w, http.StatusServiceUnavailable, "Android app links are not configured")
		return
	}
	writeAppLinksJSON(w, []map[string]any{{
		"relation": []string{"delegate_permission/common.handle_all_urls"},
		"target": map[string]any{
			"namespace":                "android_app",
			"package_name":             h.cfg.Sharing.AndroidPackageName,
			"sha256_cert_fingerprints": h.cfg.Sharing.AndroidSHA256CertFingerprints,
		},
	}})
}

func writeAppLinksJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = json.NewEncoder(w).Encode(body)
}
