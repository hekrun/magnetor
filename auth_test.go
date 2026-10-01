package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthStoreCreatesSingleAccountPersistsSessionsAndChangesPassword(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "accounts.sqlite")
	store, err := openAuthStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}

	count, err := store.userCount(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("initial user count = %d, error = %v", count, err)
	}
	password := "correct-horse-battery"
	if err := store.createFirstUser(context.Background(), "owner", password); err != nil {
		t.Fatal(err)
	}
	if err := store.createFirstUser(context.Background(), "second", password); err == nil {
		t.Fatal("expected setup to reject a second account")
	}
	if !store.checkPassword(context.Background(), "owner", password) {
		t.Fatal("expected correct password to authenticate")
	}
	if store.checkPassword(context.Background(), "owner", "wrong-password") {
		t.Fatal("expected incorrect password to be rejected")
	}
	if err := store.updateProfile(context.Background(), "new-owner", password); err != nil {
		t.Fatal(err)
	}
	currentProfile, err := store.getProfile(context.Background())
	if err != nil || currentProfile.Username != "new-owner" {
		t.Fatalf("profile = %#v, error = %v", currentProfile, err)
	}

	token, err := store.createSession(context.Background(), "new-owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	store, err = openAuthStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
	if username, ok := store.currentUser(request); !ok || username != "new-owner" {
		t.Fatalf("session user = %q, authenticated = %v", username, ok)
	}

	newPassword := "a-new-correct-horse-password"
	if err := store.changePassword(context.Background(), password, newPassword); err != nil {
		t.Fatal(err)
	}
	if !store.checkPassword(context.Background(), "new-owner", newPassword) {
		t.Fatal("expected changed password to authenticate")
	}
	if store.checkPassword(context.Background(), "new-owner", password) {
		t.Fatal("expected old password to be rejected")
	}
	if _, ok := store.currentUser(request); ok {
		t.Fatal("expected password change to invalidate existing sessions")
	}
}

func TestAuthRegistrationNeedsOnlyUsernameAndPasswordAndAllowsOneUser(t *testing.T) {
	store, err := openAuthStore(filepath.Join(t.TempDir(), "accounts.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	app := &app{auth: store}

	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"owner","password":"a-long-correct-password"}`))
	response := httptest.NewRecorder()
	app.handleAuthRegister(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("registration status = %d, body = %s", response.Code, response.Body.String())
	}

	statusResponse := httptest.NewRecorder()
	app.handleAuthStatus(statusResponse, httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))
	var status struct {
		SetupRequired bool `json:"setupRequired"`
	}
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.SetupRequired {
		t.Fatal("account setup remained open after registration")
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"second","password":"a-long-correct-password"}`))
	secondResponse := httptest.NewRecorder()
	app.handleAuthRegister(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("second registration status = %d, want %d", secondResponse.Code, http.StatusConflict)
	}
}

func TestAdminPasswordResetChangesPasswordAndRevokesSessions(t *testing.T) {
	store, err := openAuthStore(filepath.Join(t.TempDir(), "accounts.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	oldPassword := "correct-horse-battery"
	if err := store.createFirstUser(context.Background(), "owner", oldPassword); err != nil {
		t.Fatal(err)
	}
	session, err := store.createSession(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: authCookieName, Value: session})
	if _, ok := store.currentUser(request); !ok {
		t.Fatal("expected session to be valid before password reset")
	}

	newPassword := "a-new-administrator-password"
	if err := store.resetPassword(context.Background(), newPassword); err != nil {
		t.Fatal(err)
	}
	if !store.checkPassword(context.Background(), "owner", newPassword) {
		t.Fatal("expected the new password to authenticate")
	}
	if store.checkPassword(context.Background(), "owner", oldPassword) {
		t.Fatal("expected the old password to be rejected")
	}
	if _, ok := store.currentUser(request); ok {
		t.Fatal("expected password reset to revoke existing sessions")
	}
}

func TestAdminCommandRequiresSubcommand(t *testing.T) {
	if err := runAdminCommand(nil); err == nil || err.Error() != "usage: ctd username | ctd password" {
		t.Fatalf("no-argument command error = %v", err)
	}
}

func TestOpenAuthStoreDropsLegacyMailSchemaAndPreservesAccount(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "accounts.sqlite")
	store, err := openAuthStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	password := "correct-horse-battery"
	if err := store.createFirstUser(context.Background(), "owner", password); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT ''`,
		`UPDATE users SET email = 'legacy@example.com' WHERE id = 1`,
		`CREATE TABLE smtp_settings (id INTEGER PRIMARY KEY, password TEXT)`,
		`CREATE TABLE mail_settings (id INTEGER PRIMARY KEY, from_email TEXT, public_url TEXT)`,
		`CREATE TABLE password_resets (token_hash TEXT PRIMARY KEY, user_id INTEGER, expires_at INTEGER)`,
	} {
		if _, err := store.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}

	store, err = openAuthStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if !store.checkPassword(context.Background(), "owner", password) {
		t.Fatal("legacy migration did not preserve the account password")
	}
	rows, err := store.db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "email" {
			t.Fatal("legacy recovery email column remains")
		}
	}
	for _, table := range []string{"smtp_settings", "mail_settings", "password_resets"} {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("legacy table %q remains", table)
		}
	}
}

func TestRequireLoginProtectsAppRoutesAndAllowsLoginAssets(t *testing.T) {
	store, err := openAuthStore(filepath.Join(t.TempDir(), "accounts.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	app := &app{auth: store}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := app.requireLogin(next)

	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "/api/torrents", nil))
	if apiResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated API status = %d, want %d", apiResponse.Code, http.StatusUnauthorized)
	}

	pageResponse := httptest.NewRecorder()
	handler.ServeHTTP(pageResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if pageResponse.Code != http.StatusSeeOther || pageResponse.Header().Get("Location") != "/login.html" {
		t.Fatalf("unauthenticated page response = %d %q", pageResponse.Code, pageResponse.Header().Get("Location"))
	}

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodGet, "/login.html", nil))
	if loginResponse.Code != http.StatusNoContent {
		t.Fatalf("login page status = %d, want %d", loginResponse.Code, http.StatusNoContent)
	}
}
