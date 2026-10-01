package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	authCookieName  = "cloud_torrent_session"
	sessionLifetime = 30 * 24 * time.Hour
)

type authStore struct {
	db *sql.DB
}

type authCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type profile struct {
	Username string `json:"username"`
}

func openAuthStore(filename string) (*authStore, error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(filename, 0o600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token_hash TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expires_at)`,
	} {
		_, err = db.Exec(statement)
		if err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(`PRAGMA secure_delete = ON`); err != nil {
		db.Close()
		return nil, err
	}
	for _, table := range []string{"smtp_settings", "mail_settings", "password_resets"} {
		if _, err := db.Exec(`DROP TABLE IF EXISTS ` + table); err != nil {
			db.Close()
			return nil, err
		}
	}
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	hasEmail := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			db.Close()
			return nil, err
		}
		if name == "email" {
			hasEmail = true
		}
	}
	if err := rows.Close(); err != nil {
		db.Close()
		return nil, err
	}
	if hasEmail {
		if _, err := db.Exec(`ALTER TABLE users DROP COLUMN email`); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &authStore{db: db}, nil
}

func (store *authStore) close() error { return store.db.Close() }

func (store *authStore) userCount(ctx context.Context) (int, error) {
	var count int
	err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (store *authStore) currentUser(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(authCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	tokenHash := hashSessionToken(cookie.Value)
	var username string
	err = store.db.QueryRowContext(r.Context(), `
		SELECT users.username
		FROM sessions JOIN users ON users.id = sessions.user_id
		WHERE sessions.token_hash = ? AND sessions.expires_at > ?`, tokenHash, time.Now().Unix()).Scan(&username)
	return username, err == nil
}

func (store *authStore) createFirstUser(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return errors.New("username must be between 3 and 64 characters")
	}
	if len(password) < 12 || len(password) > 72 {
		return errors.New("password must be between 12 and 72 bytes")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("account setup is already complete")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO users (id, username, password_hash, created_at) VALUES (1, ?, ?, ?)`, username, string(passwordHash), time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *authStore) checkPassword(ctx context.Context, username, password string) bool {
	var passwordHash string
	err := store.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE username = ?`, strings.TrimSpace(username)).Scan(&passwordHash)
	if err != nil {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
}

func (store *authStore) getProfile(ctx context.Context) (profile, error) {
	var result profile
	err := store.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = 1`).Scan(&result.Username)
	return result, err
}

func (store *authStore) updateProfile(ctx context.Context, username, currentPassword string) error {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return errors.New("username must be between 3 and 64 characters")
	}
	if !store.checkPasswordForID(ctx, 1, currentPassword) {
		return errors.New("current password is incorrect")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET username = ? WHERE id = 1`, username); err != nil {
		return errors.New("could not update profile; the username may already be in use")
	}
	return tx.Commit()
}

func (store *authStore) checkPasswordForID(ctx context.Context, userID int64, password string) bool {
	var passwordHash string
	if store.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&passwordHash) != nil {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
}

func (store *authStore) changePassword(ctx context.Context, currentPassword, newPassword string) error {
	if !store.checkPasswordForID(ctx, 1, currentPassword) {
		return errors.New("current password is incorrect")
	}
	return store.resetPassword(ctx, newPassword)
}

func (store *authStore) resetPassword(ctx context.Context, newPassword string) error {
	if len(newPassword) < 12 || len(newPassword) > 72 {
		return errors.New("new password must be between 12 and 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = 1`, string(hash)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *authStore) createSession(ctx context.Context, username string) (string, error) {
	_, _ = store.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	var userID int64
	if err := store.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, username).Scan(&userID); err != nil {
		return "", err
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	_, err := store.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, hashSessionToken(token), userID, time.Now().Add(sessionLifetime).Unix())
	if err != nil {
		return "", err
	}
	return token, nil
}

func (store *authStore) deleteSession(ctx context.Context, token string) {
	if token != "" {
		_, _ = store.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashSessionToken(token))
	}
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name: authCookieName, Value: token, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
}

func decodeCredentials(w http.ResponseWriter, r *http.Request) (authCredentials, error) {
	var credentials authCredentials
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&credentials); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return credentials, err
	}
	if strings.TrimSpace(credentials.Username) == "" || credentials.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return credentials, errors.New("missing credentials")
	}
	return credentials, nil
}

func (a *app) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	count, err := a.auth.userCount(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read account state")
		return
	}
	username, authenticated := a.auth.currentUser(r)
	writeJSON(w, map[string]any{"setupRequired": count == 0, "authenticated": authenticated, "username": username})
}

func (a *app) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	credentials, err := decodeCredentials(w, r)
	if err != nil {
		return
	}
	if err := a.auth.createFirstUser(r.Context(), credentials.Username, credentials.Password); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	a.createLoginResponse(w, r, strings.TrimSpace(credentials.Username))
}

func (a *app) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	credentials, err := decodeCredentials(w, r)
	if err != nil {
		return
	}
	if !a.auth.checkPassword(r.Context(), credentials.Username, credentials.Password) {
		writeError(w, http.StatusUnauthorized, "username or password is incorrect")
		return
	}
	a.createLoginResponse(w, r, strings.TrimSpace(credentials.Username))
}

func (a *app) createLoginResponse(w http.ResponseWriter, r *http.Request, username string) {
	token, err := a.auth.createSession(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	setSessionCookie(w, r, token, int(sessionLifetime.Seconds()))
	writeJSON(w, map[string]any{"ok": true, "username": username})
}

func (a *app) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if cookie, err := r.Cookie(authCookieName); err == nil {
		a.auth.deleteSession(r.Context(), cookie.Value)
	}
	setSessionCookie(w, r, "", -1)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		public := r.URL.Path == "/api/health" || r.URL.Path == "/api/auth/status" ||
			r.URL.Path == "/api/auth/register" || r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/logout" ||
			r.URL.Path == "/login.html" || r.URL.Path == "/login.js" || r.URL.Path == "/auth.js" ||
			r.URL.Path == "/app.js" || r.URL.Path == "/settings.js" || r.URL.Path == "/profile.js" || r.URL.Path == "/styles.css"
		if public {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := a.auth.currentUser(r); !ok {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/download") || strings.HasPrefix(r.URL.Path, "/stream/") {
				writeError(w, http.StatusUnauthorized, "login required")
				return
			}
			http.Redirect(w, r, "/login.html", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
