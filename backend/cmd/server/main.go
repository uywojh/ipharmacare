package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Patient struct {
	ID           int64     `json:"id"`
	FullName     string    `json:"full_name"`
	Phone        string    `json:"phone"`
	DateOfBirth  *string   `json:"date_of_birth,omitempty"`
	Sex          string    `json:"sex,omitempty"`
	Allergies    string    `json:"allergies,omitempty"`
	EmergencyContact string `json:"emergency_contact,omitempty"`
	Notes        string    `json:"notes,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type API struct { db *sql.DB }

func main() {
	db, err := sql.Open("sqlite", env("DATABASE_URL", "file:ipharmacare.db?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"))
	if err != nil { log.Fatal(err) }
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil { log.Fatal(err) }
	if err := migrate(db); err != nil { log.Fatal(err) }

	api := &API{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", api.health)
	mux.HandleFunc("GET /api/patients", api.listPatients)
	mux.HandleFunc("POST /api/patients", api.createPatient)
	mux.HandleFunc("GET /api/patients/{id}", api.getPatient)
	mux.HandleFunc("PUT /api/patients/{id}", api.updatePatient)
	mux.HandleFunc("DELETE /api/patients/{id}", api.deletePatient)
	addr := ":" + env("PORT", "8080")
	log.Printf("API listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS patients (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		full_name TEXT NOT NULL CHECK(length(trim(full_name)) BETWEEN 1 AND 120),
		phone TEXT NOT NULL UNIQUE CHECK(length(trim(phone)) BETWEEN 5 AND 32),
		date_of_birth TEXT,
		sex TEXT NOT NULL DEFAULT '' CHECK(sex IN ('', 'female', 'male', 'other', 'undisclosed')),
		allergies TEXT NOT NULL DEFAULT '',
		emergency_contact TEXT NOT NULL DEFAULT '',
		notes TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	)`)
	return err
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	if err := a.db.PingContext(r.Context()); err != nil { writeError(w, 503, "database unavailable"); return }
	writeJSON(w, 200, map[string]string{"status":"ok"})
}

func (a *API) listPatients(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,full_name,phone,date_of_birth,sex,allergies,emergency_contact,notes,created_at,updated_at FROM patients ORDER BY id DESC LIMIT 200`)
	if err != nil { writeError(w, 500, "could not list patients"); return }
	defer rows.Close()
	out := make([]Patient, 0)
	for rows.Next() {
		p, err := scanPatient(rows)
		if err != nil { writeError(w, 500, "could not read patients"); return }
		out = append(out, p)
	}
	if err := rows.Err(); err != nil { writeError(w, 500, "could not read patients"); return }
	writeJSON(w, 200, out)
}

func (a *API) getPatient(w http.ResponseWriter, r *http.Request) {
	p, err := a.findPatient(r, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) { writeError(w, 404, "patient not found"); return }
	if err != nil { writeError(w, 400, "invalid patient id"); return }
	writeJSON(w, 200, p)
}

func (a *API) createPatient(w http.ResponseWriter, r *http.Request) {
	var p Patient
	if !decodePatient(w, r, &p) { return }
	p.FullName, p.Phone = strings.TrimSpace(p.FullName), strings.TrimSpace(p.Phone)
	if p.FullName == "" || p.Phone == "" { writeError(w, 400, "full_name and phone are required"); return }
	result, err := a.db.ExecContext(r.Context(), `INSERT INTO patients(full_name,phone,date_of_birth,sex,allergies,emergency_contact,notes) VALUES(?,?,?,?,?,?,?)`, p.FullName,p.Phone,p.DateOfBirth,p.Sex,p.Allergies,p.EmergencyContact,p.Notes)
	if err != nil { writeError(w, 409, "patient could not be created; phone may already exist"); return }
	p.ID, _ = result.LastInsertId()
	p, err = a.findPatient(r, intString(p.ID))
	if err != nil { writeError(w, 500, "patient created but could not be read"); return }
	writeJSON(w, 201, p)
}

func (a *API) updatePatient(w http.ResponseWriter, r *http.Request) {
	var p Patient
	if !decodePatient(w, r, &p) { return }
	p.FullName, p.Phone = strings.TrimSpace(p.FullName), strings.TrimSpace(p.Phone)
	if p.FullName == "" || p.Phone == "" { writeError(w, 400, "full_name and phone are required"); return }
	result, err := a.db.ExecContext(r.Context(), `UPDATE patients SET full_name=?,phone=?,date_of_birth=?,sex=?,allergies=?,emergency_contact=?,notes=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, p.FullName,p.Phone,p.DateOfBirth,p.Sex,p.Allergies,p.EmergencyContact,p.Notes,r.PathValue("id"))
	if err != nil { writeError(w, 409, "patient could not be updated; check id and phone"); return }
	n, _ := result.RowsAffected()
	if n == 0 { writeError(w, 404, "patient not found"); return }
	p, err = a.findPatient(r, r.PathValue("id"))
	if err != nil { writeError(w, 500, "could not read updated patient"); return }
	writeJSON(w, 200, p)
}

func (a *API) deletePatient(w http.ResponseWriter, r *http.Request) {
	result, err := a.db.ExecContext(r.Context(), "DELETE FROM patients WHERE id=?", r.PathValue("id"))
	if err != nil { writeError(w, 500, "could not delete patient"); return }
	n, _ := result.RowsAffected()
	if n == 0 { writeError(w, 404, "patient not found"); return }
	w.WriteHeader(http.StatusNoContent)
}

type scanner interface { Scan(...any) error }
func scanPatient(s scanner) (Patient,error) {
	var p Patient
	var dob sql.NullString
	err:=s.Scan(&p.ID,&p.FullName,&p.Phone,&dob,&p.Sex,&p.Allergies,&p.EmergencyContact,&p.Notes,&p.CreatedAt,&p.UpdatedAt)
	if dob.Valid { p.DateOfBirth=&dob.String }
	return p,err
}
func (a *API) findPatient(r *http.Request,id string)(Patient,error) {
	row:=a.db.QueryRowContext(r.Context(),`SELECT id,full_name,phone,date_of_birth,sex,allergies,emergency_contact,notes,created_at,updated_at FROM patients WHERE id=?`,id)
	return scanPatient(row)
}
func decodePatient(w http.ResponseWriter,r *http.Request,p *Patient) bool {
	r.Body=http.MaxBytesReader(w,r.Body,1<<20)
	d:=json.NewDecoder(r.Body); d.DisallowUnknownFields()
	if err:=d.Decode(p); err!=nil { writeError(w,400,"invalid JSON body"); return false }
	return true
}
func writeJSON(w http.ResponseWriter,status int,v any){w.Header().Set("Content-Type","application/json; charset=utf-8");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(v)}
func writeError(w http.ResponseWriter,status int,message string){writeJSON(w,status,map[string]string{"error":message})}
func intString(n int64) string { return fmt.Sprintf("%d",n) }
func env(key,fallback string)string{if v:=os.Getenv(key);v!=""{return v};return fallback}
