package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type App struct{ db *sql.DB }
type credentials struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Password string `json:"password"`
	Captcha  string `json:"captcha"`
}

func main() {
	db, err := sql.Open("sqlite", env("DATABASE_URL", "file:ipharmacare.db?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS patients(id INTEGER PRIMARY KEY,full_name TEXT NOT NULL,phone TEXT NOT NULL UNIQUE,password_hash BLOB NOT NULL,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP); CREATE TABLE IF NOT EXISTS sessions(token_hash TEXT PRIMARY KEY,patient_id INTEGER NOT NULL REFERENCES patients(id) ON DELETE CASCADE,expires_at TEXT NOT NULL);`)
	if err != nil {
		log.Fatal(err)
	}
	a := &App{db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/captcha", captchaImage)
	mux.HandleFunc("POST /api/auth/register", a.register)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.logout)
	mux.HandleFunc("GET /api/auth/session", a.session)
	log.Fatal(http.ListenAndServe(":"+env("PORT", "8080"), mux))
}
func (a *App) register(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil || !verifyCaptcha(r, c.Captcha) || len(c.Password) < 10 || strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Phone) == "" {
		http.Error(w, "请填写姓名、手机号及至少10位密码", 400)
		return
	}
	h, e := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if e != nil {
		http.Error(w, "服务错误", 500)
		return
	}
	res, e := a.db.ExecContext(r.Context(), "INSERT INTO patients(full_name,phone,password_hash) VALUES(?,?,?)", strings.TrimSpace(c.Name), strings.TrimSpace(c.Phone), h)
	if e != nil {
		http.Error(w, "注册失败，请检查手机号是否已使用", 409)
		return
	}
	id, _ := res.LastInsertId()
	a.newSession(w, r, id)
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		http.Error(w, "请求格式错误", 400)
		return
	}
	if !verifyCaptcha(r, c.Captcha) {
		http.Error(w, "验证码错误或已过期", 400)
		return
	}
	var id int64
	var hash []byte
	e := a.db.QueryRowContext(r.Context(), "SELECT id,password_hash FROM patients WHERE phone=?", strings.TrimSpace(c.Phone)).Scan(&id, &hash)
	if e != nil || bcrypt.CompareHashAndPassword(hash, []byte(c.Password)) != nil {
		http.Error(w, "手机号或密码错误", 401)
		return
	}
	a.newSession(w, r, id)
}
func (a *App) newSession(w http.ResponseWriter, r *http.Request, id int64) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		http.Error(w, "服务错误", 500)
		return
	}
	token := hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(token))
	expires := time.Now().Add(7 * 24 * time.Hour)
	_, e := a.db.ExecContext(r.Context(), "INSERT INTO sessions(token_hash,patient_id,expires_at) VALUES(?,?,?)", hex.EncodeToString(sum[:]), id, expires.UTC().Format(time.RFC3339))
	if e != nil {
		http.Error(w, "服务错误", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: os.Getenv("COOKIE_SECURE") == "true", Expires: expires})
	json.NewEncoder(w).Encode(map[string]bool{"authenticated": true})
}
func (a *App) session(w http.ResponseWriter, r *http.Request) {
	c, e := r.Cookie("session")
	if e != nil {
		http.Error(w, "未登录", 401)
		return
	}
	s := sha256.Sum256([]byte(c.Value))
	var id int64
	e = a.db.QueryRowContext(r.Context(), "SELECT patient_id FROM sessions WHERE token_hash=? AND expires_at>?", hex.EncodeToString(s[:]), time.Now().UTC().Format(time.RFC3339)).Scan(&id)
	if e != nil {
		http.Error(w, "未登录", 401)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"authenticated": true, "patient_id": id})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("session"); e == nil {
		s := sha256.Sum256([]byte(c.Value))
		_, _ = a.db.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=?", hex.EncodeToString(s[:]))
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(204)
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
