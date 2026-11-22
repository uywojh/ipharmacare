package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

func testApp(t *testing.T) *App {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE patients(id INTEGER PRIMARY KEY,full_name TEXT NOT NULL,phone TEXT NOT NULL UNIQUE,password_hash BLOB NOT NULL,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP); CREATE TABLE sessions(token_hash TEXT PRIMARY KEY,patient_id INTEGER NOT NULL REFERENCES patients(id) ON DELETE CASCADE,expires_at TEXT NOT NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &App{db: db}
}
func requestJSON(t *testing.T, h http.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	imageResponse := httptest.NewRecorder()
	imageRequest := httptest.NewRequest("GET", "/api/auth/captcha", nil)
	captchaImage(imageResponse, imageRequest)
	cookie := imageResponse.Result().Cookies()[0]
	challenges.Lock()
	code := challenges.m[cookie.Value].code
	challenges.Unlock()
	if c, ok := body.(credentials); ok {
		c.Captcha = code
		body = c
	}
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/", bytes.NewReader(b))
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h(w, r)
	return w
}
func TestRegisterCreatesHashedAccountAndSession(t *testing.T) {
	a := testApp(t)
	w := requestJSON(t, a.register, credentials{Name: "测试患者甲", Phone: "13800000001", Password: "test-pass-123"})
	if w.Code != 200 {
		t.Fatalf("register status=%d body=%s", w.Code, w.Body)
	}
	cookie := w.Result().Cookies()[0]
	if cookie.Value == "" || !cookie.HttpOnly {
		t.Fatal("expected HttpOnly session cookie")
	}
	var hash []byte
	if err := a.db.QueryRow("SELECT password_hash FROM patients WHERE phone=?", "13800000001").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(hash, []byte("test-pass-123")) {
		t.Fatal("password stored in plaintext")
	}
}
func TestLoginSuccessAndWrongPassword(t *testing.T) {
	a := testApp(t)
	requestJSON(t, a.register, credentials{Name: "测试患者乙", Phone: "13800000002", Password: "test-pass-456"})
	ok := requestJSON(t, a.login, credentials{Phone: "13800000002", Password: "test-pass-456"})
	if ok.Code != 200 {
		t.Fatalf("login status=%d", ok.Code)
	}
	bad := requestJSON(t, a.login, credentials{Phone: "13800000002", Password: "wrong-pass-000"})
	if bad.Code != 401 {
		t.Fatalf("wrong password status=%d", bad.Code)
	}
}
func TestDuplicatePhoneAndWeakPasswordRejected(t *testing.T) {
	a := testApp(t)
	requestJSON(t, a.register, credentials{Name: "测试患者丙", Phone: "13800000003", Password: "test-pass-789"})
	dup := requestJSON(t, a.register, credentials{Name: "重复号码", Phone: "13800000003", Password: "test-pass-999"})
	if dup.Code != 409 {
		t.Fatalf("duplicate status=%d", dup.Code)
	}
	weak := requestJSON(t, a.register, credentials{Name: "弱密码", Phone: "13800000004", Password: "short"})
	if weak.Code != 400 {
		t.Fatalf("weak password status=%d", weak.Code)
	}
}
func TestSessionAndLogoutRegression(t *testing.T) {
	a := testApp(t)
	login := requestJSON(t, a.register, credentials{Name: "回归患者", Phone: "13800000005", Password: "test-pass-abc"})
	cookie := login.Result().Cookies()[0]
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.session(w, r)
	if w.Code != 200 {
		t.Fatalf("session status=%d body=%s", w.Code, w.Body)
	}
	lr := httptest.NewRequest("POST", "/", nil)
	lr.AddCookie(cookie)
	lw := httptest.NewRecorder()
	a.logout(lw, lr)
	sr := httptest.NewRequest("GET", "/", nil)
	sr.AddCookie(cookie)
	sw := httptest.NewRecorder()
	a.session(sw, sr)
	if sw.Code != 401 {
		t.Fatalf("session after logout status=%d", sw.Code)
	}
}

func TestCaptchaImageAndSingleUseValidation(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/auth/captcha", nil)
	captchaImage(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !bytes.HasPrefix(w.Body.Bytes(), []byte{137, 80, 78, 71}) {
		t.Fatalf("bad captcha image: status=%d type=%s", w.Code, w.Header().Get("Content-Type"))
	}
	cookie := w.Result().Cookies()[0]
	challenges.Lock()
	code := challenges.m[cookie.Value].code
	challenges.Unlock()
	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(cookie)
	if !verifyCaptcha(req, code) {
		t.Fatal("valid captcha rejected")
	}
	if verifyCaptcha(req, code) {
		t.Fatal("captcha was reusable")
	}
}

func TestAuthHTTPRoutesRegression(t *testing.T) {
	a := testApp(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/captcha", captchaImage)
	mux.HandleFunc("POST /api/auth/register", a.register)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.logout)
	mux.HandleFunc("GET /api/auth/session", a.session)
	getChallenge := func() (string, *http.Cookie) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/auth/captcha", nil))
		if w.Code != 200 || !bytes.HasPrefix(w.Body.Bytes(), []byte{137, 80, 78, 71}) {
			t.Fatalf("captcha endpoint status=%d", w.Code)
		}
		cookie := w.Result().Cookies()[0]
		challenges.Lock()
		code := challenges.m[cookie.Value].code
		challenges.Unlock()
		return code, cookie
	}
	code, cookie := getChallenge()
	payload, _ := json.Marshal(credentials{Name: "回归患者", Phone: "13800000991", Password: "Regression-123", Captcha: code})
	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(payload))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("register API status=%d %s", w.Code, w.Body)
	}
	sessionCookie := w.Result().Cookies()[0]
	code, cookie = getChallenge()
	payload, _ = json.Marshal(credentials{Phone: "13800000991", Password: "Regression-123", Captcha: code})
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(payload))
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("login API status=%d %s", w.Code, w.Body)
	}
	req = httptest.NewRequest("GET", "/api/auth/session", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("session API status=%d", w.Code)
	}
	req = httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	req = httptest.NewRequest("GET", "/api/auth/session", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("logout regression status=%d", w.Code)
	}
}
