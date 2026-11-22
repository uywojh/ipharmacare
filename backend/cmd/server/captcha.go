package main

import (
	"crypto/rand"
	"encoding/hex"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

var challenges = struct {
	sync.Mutex
	m map[string]challenge
}{m: map[string]challenge{}}

type challenge struct {
	code    string
	expires time.Time
}

var digitBits = [10][7]string{{"01110", "10001", "10011", "10101", "11001", "10001", "01110"}, {"00100", "01100", "00100", "00100", "00100", "00100", "01110"}, {"01110", "10001", "00001", "00010", "00100", "01000", "11111"}, {"11110", "00001", "00001", "01110", "00001", "00001", "11110"}, {"00010", "00110", "01010", "10010", "11111", "00010", "00010"}, {"11111", "10000", "10000", "11110", "00001", "00001", "11110"}, {"01110", "10000", "10000", "11110", "10001", "10001", "01110"}, {"11111", "00001", "00010", "00100", "01000", "01000", "01000"}, {"01110", "10001", "10001", "01110", "10001", "10001", "01110"}, {"01110", "10001", "10001", "01111", "00001", "00001", "01110"}}

func captchaImage(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	n, _ := rand.Int(rand.Reader, big.NewInt(10000))
	code := strings.Repeat("0", 4-len(n.String())) + n.String()
	challenges.Lock()
	now := time.Now()
	for k, v := range challenges.m {
		if now.After(v.expires) {
			delete(challenges.m, k)
		}
	}
	challenges.m[id] = challenge{code, now.Add(3 * time.Minute)}
	challenges.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "captcha_id", Value: id, Path: "/api/auth", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 180})
	img := image.NewRGBA(image.Rect(0, 0, 150, 52))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{245, 248, 245, 255}}, image.Point{}, draw.Src)
	for i := 0; i < 12; i++ {
		x, _ := rand.Int(rand.Reader, big.NewInt(150))
		y, _ := rand.Int(rand.Reader, big.NewInt(52))
		col := color.RGBA{uint8(80 + i*7), uint8(120 + i*5), uint8(105 + i*6), 255}
		for j := -8; j <= 8; j++ {
			px := int(x.Int64()) + j
			py := int(y.Int64()) + j/2
			if image.Pt(px, py).In(img.Bounds().Inset(1)) {
				img.Set(px, py, col)
			}
		}
	}
	for i, ch := range code {
		drawDigit(img, int(ch-'0'), 12+i*34, 14)
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_ = png.Encode(w, img)
}
func drawDigit(img *image.RGBA, d, x, y int) {
	for row, line := range digitBits[d] {
		for col, p := range line {
			if p == '1' {
				for dy := 0; dy < 3; dy++ {
					for dx := 0; dx < 3; dx++ {
						px, py := x+col*3+dx, y+row*3+dy
						if image.Pt(px, py).In(img.Bounds()) {
							img.Set(px, py, color.RGBA{25, 75, 61, 255})
						}
					}
				}
			}
		}
	}
}
func verifyCaptcha(r *http.Request, input string) bool {
	c, e := r.Cookie("captcha_id")
	if e != nil {
		return false
	}
	challenges.Lock()
	defer challenges.Unlock()
	v, ok := challenges.m[c.Value]
	delete(challenges.m, c.Value)
	return ok && time.Now().Before(v.expires) && strings.TrimSpace(input) == v.code
}
