package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookieName = "registry_control_session"

type loginPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type changePasswordPayload struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

func (a *App) ensureBootstrapAdmin(ctx context.Context) error {
	count, err := a.countQuery(ctx, "SELECT COUNT(*) FROM users")
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if strings.TrimSpace(a.cfg.BootstrapPassword) == "" {
		return fmt.Errorf("CONTROL_BOOTSTRAP_PASSWORD is required on first startup when no users exist")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(a.cfg.BootstrapPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := nowRFC3339()
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, must_change_password, failed_attempts, created_at, updated_at, last_password_change_at)
		VALUES (?, ?, ?, 0, ?, ?, ?)
	`, a.cfg.BootstrapUsername, string(hash), boolToInt(a.cfg.BootstrapForcePasswordChange), now, now, now)
	if err != nil {
		return err
	}
	a.logSystem(ctx, "info", "auth", a.cfg.BootstrapUsername, "bootstrap admin created", map[string]any{
		"must_change_password": a.cfg.BootstrapForcePasswordChange,
	})
	return nil
}

func (a *App) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if auth, ok := a.authSessionFromRequest(r); ok {
		if auth.User.MustChangePassword {
			http.Redirect(w, r, "/force-password", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.templates.ExecuteTemplate(w, "login.html", map[string]any{
		"Title": "เข้าสู่ระบบ",
	})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	var payload loginPayload
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
			a.writeError(w, http.StatusBadRequest, "ข้อมูลเข้าสู่ระบบไม่ถูกต้อง")
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			a.writeError(w, http.StatusBadRequest, "ข้อมูลเข้าสู่ระบบไม่ถูกต้อง")
			return
		}
		payload.Username = strings.TrimSpace(r.FormValue("username"))
		payload.Password = r.FormValue("password")
	}
	if payload.Username == "" || payload.Password == "" {
		a.writeError(w, http.StatusBadRequest, "กรุณากรอกชื่อผู้ใช้และรหัสผ่านให้ครบ")
		return
	}

	user, err := a.findUserByUsername(r.Context(), payload.Username)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			a.writeError(w, http.StatusInternalServerError, "ตรวจสอบบัญชีผู้ใช้ไม่สำเร็จ")
			return
		}
		time.Sleep(450 * time.Millisecond)
		a.logSystem(r.Context(), "warn", "auth", payload.Username, "login failed", map[string]any{
			"reason": "user_not_found",
			"remote": r.RemoteAddr,
		})
		a.writeError(w, http.StatusUnauthorized, "ชื่อผู้ใช้หรือรหัสผ่านไม่ถูกต้อง")
		return
	}

	if lockedUntil, locked := userLocked(user); locked {
		a.logSystem(r.Context(), "warn", "auth", user.Username, "login blocked", map[string]any{
			"reason":       "locked",
			"locked_until": lockedUntil,
			"remote":       r.RemoteAddr,
		})
		a.writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":        "บัญชีถูกล็อกชั่วคราวจากการพยายามเข้าสู่ระบบหลายครั้ง",
			"locked_until": lockedUntil,
		})
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(payload.Password)) != nil {
		_ = a.recordFailedLogin(r.Context(), user)
		time.Sleep(450 * time.Millisecond)
		a.logSystem(r.Context(), "warn", "auth", user.Username, "login failed", map[string]any{
			"reason": "invalid_password",
			"remote": r.RemoteAddr,
		})
		a.writeError(w, http.StatusUnauthorized, "ชื่อผู้ใช้หรือรหัสผ่านไม่ถูกต้อง")
		return
	}

	if err := a.recordSuccessfulLogin(r.Context(), user.ID); err != nil {
		a.writeError(w, http.StatusInternalServerError, "อัปเดตสถานะการเข้าสู่ระบบไม่สำเร็จ")
		return
	}

	sessionToken, session, err := a.createSession(r.Context(), user.ID, r.RemoteAddr, r.UserAgent())
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, "สร้าง session ไม่สำเร็จ")
		return
	}

	http.SetCookie(w, a.sessionCookie(sessionToken, session.ExpiresAt))
	a.logSystem(r.Context(), "info", "auth", user.Username, "login success", map[string]any{
		"remote":               r.RemoteAddr,
		"must_change_password": user.MustChangePassword,
	})

	redirectPath := "/dashboard"
	if user.MustChangePassword {
		redirectPath = "/force-password"
	}
	a.writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"redirect":             redirectPath,
		"must_change_password": user.MustChangePassword,
	})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !a.sameOrigin(r) {
		a.writeError(w, http.StatusForbidden, "origin ไม่ถูกต้อง")
		return
	}
	auth, ok := a.authSessionFromRequest(r)
	if ok {
		_ = a.revokeSessionByID(r.Context(), auth.Session.ID)
		a.logSystem(r.Context(), "info", "auth", auth.User.Username, "logout", map[string]any{
			"remote": r.RemoteAddr,
		})
	}
	http.SetCookie(w, a.clearSessionCookie())
	a.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	auth, ok := a.authSessionFromRequest(r)
	if !ok {
		a.writeError(w, http.StatusUnauthorized, "ต้องเข้าสู่ระบบก่อน")
		return
	}
	maintenance, _ := a.getMaintenanceState(r.Context())
	a.writeJSON(w, http.StatusOK, map[string]any{
		"username":             auth.User.Username,
		"must_change_password": auth.User.MustChangePassword,
		"session_expires_at":   auth.Session.ExpiresAt,
		"csrf_token":           auth.Session.CSRFToken,
		"maintenance_mode":     maintenance.MaintenanceMode,
	})
}

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	auth, ok := a.authSessionFromRequest(r)
	if !ok {
		a.writeError(w, http.StatusUnauthorized, "ต้องเข้าสู่ระบบก่อน")
		return
	}

	var payload changePasswordPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		a.writeError(w, http.StatusBadRequest, "ข้อมูลเปลี่ยนรหัสผ่านไม่ถูกต้อง")
		return
	}
	if payload.NewPassword != payload.ConfirmPassword {
		a.writeError(w, http.StatusBadRequest, "รหัสผ่านใหม่และการยืนยันรหัสผ่านไม่ตรงกัน")
		return
	}
	if err := validatePasswordStrength(auth.User.Username, payload.NewPassword); err != nil {
		a.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(auth.User.PasswordHash), []byte(payload.CurrentPassword)) != nil {
		a.writeError(w, http.StatusUnauthorized, "รหัสผ่านปัจจุบันไม่ถูกต้อง")
		return
	}
	if payload.CurrentPassword == payload.NewPassword {
		a.writeError(w, http.StatusBadRequest, "รหัสผ่านใหม่ต้องไม่ซ้ำกับรหัสผ่านเดิม")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(payload.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, "เข้ารหัสรหัสผ่านใหม่ไม่สำเร็จ")
		return
	}
	if err := a.updateUserPassword(r.Context(), auth.User.ID, string(hash)); err != nil {
		a.writeError(w, http.StatusInternalServerError, "บันทึกรหัสผ่านใหม่ไม่สำเร็จ")
		return
	}
	if err := a.revokeAllSessionsForUser(r.Context(), auth.User.ID); err != nil {
		a.writeError(w, http.StatusInternalServerError, "ล้าง session เดิมไม่สำเร็จ")
		return
	}

	sessionToken, session, err := a.createSession(r.Context(), auth.User.ID, r.RemoteAddr, r.UserAgent())
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, "สร้าง session ใหม่ไม่สำเร็จ")
		return
	}
	http.SetCookie(w, a.sessionCookie(sessionToken, session.ExpiresAt))

	a.logSystem(r.Context(), "info", "auth", auth.User.Username, "password changed", map[string]any{
		"remote": r.RemoteAddr,
	})
	a.writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"redirect": "/dashboard",
	})
}

func (a *App) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, ok := a.authSessionFromRequest(r)
		if !ok {
			if stringsHasPrefix(r.URL.Path, "/api/") {
				a.writeError(w, http.StatusUnauthorized, "ต้องเข้าสู่ระบบก่อน")
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		if (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete) && !a.sameOrigin(r) {
			a.writeError(w, http.StatusForbidden, "origin ไม่ถูกต้อง")
			return
		}
		if (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete) &&
			r.URL.Path != "/auth/logout" &&
			r.URL.Path != "/api/auth/change-password" {
			if r.Header.Get("X-CSRF-Token") != auth.Session.CSRFToken {
				a.writeError(w, http.StatusForbidden, "CSRF token ไม่ถูกต้อง")
				return
			}
		}

		if auth.User.MustChangePassword {
			allowed := r.URL.Path == "/force-password" ||
				r.URL.Path == "/api/auth/me" ||
				r.URL.Path == "/api/auth/change-password" ||
				r.URL.Path == "/auth/logout"
			if !allowed {
				if stringsHasPrefix(r.URL.Path, "/api/") {
					a.writeJSON(w, http.StatusForbidden, map[string]any{
						"error":                "ต้องเปลี่ยนรหัสผ่านก่อนใช้งานส่วนอื่น",
						"must_change_password": true,
					})
					return
				}
				http.Redirect(w, r, "/force-password", http.StatusFound)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (a *App) authSessionFromRequest(r *http.Request) (AuthSession, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return AuthSession{}, false
	}
	sessionToken := cookie.Value
	auth, err := a.findAuthSession(r.Context(), sessionToken)
	if err != nil {
		return AuthSession{}, false
	}

	if err := a.touchSession(r.Context(), auth.Session.ID); err == nil {
		if updated, err := a.findSessionByID(r.Context(), auth.Session.ID); err == nil {
			auth.Session = updated
		}
	}
	return auth, true
}

func (a *App) sessionCookie(token, expiresAt string) *http.Cookie {
	expiresTime, _ := parseRFC3339(expiresAt)
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		Expires:  expiresTime,
		MaxAge:   int(a.cfg.SessionTTL.Seconds()),
	}
}

func (a *App) clearSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
}

func (a *App) sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if a.cfg.PublicBaseURL != "" {
		return origin == a.cfg.PublicBaseURL
	}

	scheme := "http"
	if r.TLS != nil || a.cfg.CookieSecure {
		scheme = "https"
	}
	host := r.Host
	if a.cfg.TrustProxyHeaders {
		if forwardedProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
			scheme = forwardedProto
		}
		if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
			host = forwardedHost
		}
	}
	return origin == scheme+"://"+host
}

func (a *App) notificationAuthorized(r *http.Request) bool {
	username, password, ok := r.BasicAuth()
	if !ok {
		return false
	}
	return username == a.cfg.NotificationsUsername && password == a.cfg.NotificationsPassword
}

func userLocked(user User) (string, bool) {
	if user.LockedUntil == nil || strings.TrimSpace(*user.LockedUntil) == "" {
		return "", false
	}
	lockedUntil, err := parseRFC3339(*user.LockedUntil)
	if err != nil {
		return "", false
	}
	return lockedUntil.Format(time.RFC3339), time.Now().UTC().Before(lockedUntil)
}
