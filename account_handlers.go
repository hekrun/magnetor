package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (a *app) handleProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		profile, err := a.auth.getProfile(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not load profile")
			return
		}
		writeJSON(w, profile)
		return
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct {
		Username        string `json:"username"`
		CurrentPassword string `json:"currentPassword"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if err := a.auth.updateProfile(r.Context(), request.Username, request.CurrentPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "username": strings.TrimSpace(request.Username)})
}

func (a *app) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if err := a.auth.changePassword(r.Context(), request.CurrentPassword, request.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	setSessionCookie(w, r, "", -1)
	writeJSON(w, map[string]bool{"ok": true})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return err
	}
	return nil
}
