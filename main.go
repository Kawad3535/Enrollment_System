package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var db *sql.DB

// ---------- password hashing (salted SHA-256; swap for bcrypt in production) ----------

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h := sha256.Sum256(append(salt, []byte(password)...))
	return hex.EncodeToString(salt) + "$" + hex.EncodeToString(h[:]), nil
}

func checkPassword(password, stored string) bool {
	parts := strings.SplitN(stored, "$", 2)
	if len(parts) != 2 {
		return false
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}
	h := sha256.Sum256(append(salt, []byte(password)...))
	return hex.EncodeToString(h[:]) == parts[1]
}

// ---------- sessions ----------

var (
	sessions = map[string]int{}
	sessMu   sync.RWMutex
)

func newToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type authedUser struct {
	ID   int
	Name string
	Role string
}

type ctxKey string

const userCtxKey ctxKey = "user"

func fromCtx(r *http.Request) authedUser {
	return r.Context().Value(userCtxKey).(authedUser)
}

func requireRole(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			token := strings.TrimPrefix(authHeader, "Bearer ")
			sessMu.RLock()
			uid, ok := sessions[token]
			sessMu.RUnlock()
			if !ok {
				writeErr(w, 401, "Not authenticated")
				return
			}
			var name, role string
			if err := db.QueryRow("SELECT name, role FROM users WHERE id=?", uid).Scan(&name, &role); err != nil {
				writeErr(w, 401, "Not authenticated")
				return
			}
			if len(roles) > 0 {
				allowed := false
				for _, rr := range roles {
					if rr == role {
						allowed = true
					}
				}
				if !allowed {
					writeErr(w, 403, "You don't have access to this part of the system")
					return
				}
			}
			ctx := context.WithValue(r.Context(), userCtxKey, authedUser{uid, name, role})
			next(w, r.WithContext(ctx))
		}
	}
}

// ---------- JSON helpers ----------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func nullStr(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// ---------- auth handlers ----------

func registerHandler(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Email, Password string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "Invalid request body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if in.Name == "" || in.Email == "" || len(in.Password) < 6 {
		writeErr(w, 400, "Name, email, and a password of at least 6 characters are required")
		return
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		writeErr(w, 500, "Could not create account")
		return
	}
	res, err := db.Exec("INSERT INTO users (name,email,password_hash,role) VALUES (?,?,?,'student')", in.Name, in.Email, hash)
	if err != nil {
		writeErr(w, 409, "An account with that email already exists")
		return
	}
	uid, _ := res.LastInsertId()
	db.Exec("INSERT INTO student_profiles (user_id) VALUES (?)", uid)
	token := newToken()
	sessMu.Lock()
	sessions[token] = int(uid)
	sessMu.Unlock()
	writeJSON(w, 200, map[string]interface{}{
		"token": token,
		"user":  map[string]interface{}{"id": uid, "name": in.Name, "role": "student"},
	})
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "Invalid request body")
		return
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	var id int
	var name, role, hash string
	err := db.QueryRow("SELECT id,name,role,password_hash FROM users WHERE email=?", in.Email).Scan(&id, &name, &role, &hash)
	if err != nil || !checkPassword(in.Password, hash) {
		writeErr(w, 401, "Incorrect email or password")
		return
	}
	token := newToken()
	sessMu.Lock()
	sessions[token] = id
	sessMu.Unlock()
	writeJSON(w, 200, map[string]interface{}{
		"token": token,
		"user":  map[string]interface{}{"id": id, "name": name, "role": role},
	})
}

// ---------- profile ----------

func meHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	resp := map[string]interface{}{"id": u.ID, "name": u.Name, "role": u.Role}
	if u.Role == "student" {
		var birthdate, address, contact, prevSchool, prevProgram sql.NullString
		db.QueryRow(`SELECT birthdate,address,contact_number,previous_school,previous_program
			FROM student_profiles WHERE user_id=?`, u.ID).
			Scan(&birthdate, &address, &contact, &prevSchool, &prevProgram)
		resp["profile"] = map[string]interface{}{
			"birthdate":         nullStr(birthdate),
			"address":           nullStr(address),
			"contact_number":    nullStr(contact),
			"previous_school":   nullStr(prevSchool),
			"previous_program":  nullStr(prevProgram),
		}
	}
	writeJSON(w, 200, resp)
}

func updateProfileHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	var in struct {
		Name, Birthdate, Address, ContactNumber, PreviousSchool, PreviousProgram string
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "Invalid request body")
		return
	}
	if strings.TrimSpace(in.Name) != "" {
		db.Exec("UPDATE users SET name=? WHERE id=?", in.Name, u.ID)
	}
	var bd interface{}
	if in.Birthdate != "" {
		bd = in.Birthdate
	} else {
		bd = nil
	}
	db.Exec(`UPDATE student_profiles SET birthdate=?, address=?, contact_number=?, previous_school=?, previous_program=? WHERE user_id=?`,
		bd, in.Address, in.ContactNumber, in.PreviousSchool, in.PreviousProgram, u.ID)
	writeJSON(w, 200, map[string]string{"status": "updated"})
}

// ---------- programs / subjects ----------

func programsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id,code,name FROM programs ORDER BY id")
	if err != nil {
		writeErr(w, 500, "Could not load programs")
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id int
		var code, name string
		rows.Scan(&id, &code, &name)
		out = append(out, map[string]interface{}{"id": id, "code": code, "name": name})
	}
	writeJSON(w, 200, out)
}

func subjectsByProgramHandler(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	rows, err := db.Query("SELECT id,code,title,units FROM subjects WHERE program_id=? ORDER BY code", pid)
	if err != nil {
		writeErr(w, 500, "Could not load subjects")
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, units int
		var code, title string
		rows.Scan(&id, &code, &title, &units)
		out = append(out, map[string]interface{}{"id": id, "code": code, "title": title, "units": units})
	}
	writeJSON(w, 200, out)
}

// ---------- enrollment application: shared read model ----------

func getFullApplication(appID int) (map[string]interface{}, error) {
	var studentID, programID int
	var studentName, programCode, programName, status string
	var deanRemarks, returnReason sql.NullString
	var createdAt, updatedAt time.Time

	err := db.QueryRow(`
		SELECT a.student_id, u.name, a.program_id, p.code, p.name, a.status, a.dean_remarks, a.return_reason, a.created_at, a.updated_at
		FROM enrollment_applications a
		JOIN users u ON u.id = a.student_id
		JOIN programs p ON p.id = a.program_id
		WHERE a.id = ?`, appID).Scan(&studentID, &studentName, &programID, &programCode, &programName, &status, &deanRemarks, &returnReason, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	subjRows, _ := db.Query(`
		SELECT s.id, s.code, s.title, s.units FROM application_subjects asub
		JOIN subjects s ON s.id = asub.subject_id WHERE asub.application_id=? ORDER BY s.code`, appID)
	subjects := []map[string]interface{}{}
	totalUnits := 0
	for subjRows.Next() {
		var id, units int
		var code, title string
		subjRows.Scan(&id, &code, &title, &units)
		subjects = append(subjects, map[string]interface{}{"id": id, "code": code, "title": title, "units": units})
		totalUnits += units
	}
	subjRows.Close()

	docRows, _ := db.Query("SELECT id, original_name, uploaded_at FROM documents WHERE application_id=? ORDER BY uploaded_at", appID)
	docs := []map[string]interface{}{}
	for docRows.Next() {
		var id int
		var name string
		var up time.Time
		docRows.Scan(&id, &name, &up)
		docs = append(docs, map[string]interface{}{"id": id, "name": name, "uploaded_at": up.Format("Jan 2, 2006 3:04 PM")})
	}
	docRows.Close()

	var payment map[string]interface{}
	var tu int
	var tf, mf, ta float64
	var pstatus string
	var confAt sql.NullTime
	if err2 := db.QueryRow("SELECT total_units,tuition_fee,misc_fee,total_amount,status,confirmed_at FROM payments WHERE application_id=?", appID).
		Scan(&tu, &tf, &mf, &ta, &pstatus, &confAt); err2 == nil {
		payment = map[string]interface{}{
			"total_units": tu, "tuition_fee": tf, "misc_fee": mf, "total_amount": ta, "status": pstatus,
		}
		if confAt.Valid {
			payment["confirmed_at"] = confAt.Time.Format("Jan 2, 2006 3:04 PM")
		}
	}

	histRows, _ := db.Query("SELECT stage,action,actor_name,actor_role,note,created_at FROM application_history WHERE application_id=? ORDER BY created_at", appID)
	history := []map[string]interface{}{}
	for histRows.Next() {
		var stage, action, actorName, actorRole string
		var note sql.NullString
		var at time.Time
		histRows.Scan(&stage, &action, &actorName, &actorRole, &note, &at)
		h := map[string]interface{}{"stage": stage, "action": action, "actor_name": actorName, "actor_role": actorRole, "at": at.Format("Jan 2, 2006 3:04 PM")}
		if note.Valid {
			h["note"] = note.String
		}
		history = append(history, h)
	}
	histRows.Close()

	var address, contact, prevSchool, prevProgram sql.NullString
	db.QueryRow("SELECT address, contact_number, previous_school, previous_program FROM student_profiles WHERE user_id=?", studentID).
		Scan(&address, &contact, &prevSchool, &prevProgram)

	prevRows, _ := db.Query(`
		SELECT p.name, a.status, a.created_at FROM enrollment_applications a
		JOIN programs p ON p.id=a.program_id WHERE a.student_id=? AND a.id<>? ORDER BY a.created_at DESC`, studentID, appID)
	priorRecords := []map[string]interface{}{}
	for prevRows.Next() {
		var pname, pstat string
		var cat time.Time
		prevRows.Scan(&pname, &pstat, &cat)
		priorRecords = append(priorRecords, map[string]interface{}{"program": pname, "status": pstat, "date": cat.Format("Jan 2, 2006")})
	}
	prevRows.Close()

	return map[string]interface{}{
		"id":     appID,
		"status": status,
		"student": map[string]interface{}{
			"id": studentID, "name": studentName,
			"address": nullStr(address), "contact_number": nullStr(contact),
			"previous_school": nullStr(prevSchool), "previous_program": nullStr(prevProgram),
		},
		"program":       map[string]interface{}{"id": programID, "code": programCode, "name": programName},
		"subjects":      subjects,
		"total_units":   totalUnits,
		"dean_remarks":  nullStr(deanRemarks),
		"return_reason": nullStr(returnReason),
		"documents":     docs,
		"payment":       payment,
		"history":       history,
		"prior_records": priorRecords,
		"created_at":    createdAt.Format("Jan 2, 2006 3:04 PM"),
		"updated_at":    updatedAt.Format("Jan 2, 2006 3:04 PM"),
	}, nil
}

func applicationDetailHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var appID int
	fmt.Sscanf(id, "%d", &appID)
	data, err := getFullApplication(appID)
	if err != nil {
		writeErr(w, 404, "Application not found")
		return
	}
	writeJSON(w, 200, data)
}

// ---------- student: submit / view / resubmit / upload ----------

func submitEnrollmentHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	var in struct {
		ProgramID int `json:"program_id"`
	}
	if err := readJSON(r, &in); err != nil || in.ProgramID == 0 {
		writeErr(w, 400, "Select a program")
		return
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM enrollment_applications WHERE student_id=? AND status <> 'enrolled'", u.ID).Scan(&count)
	if count > 0 {
		writeErr(w, 409, "You already have an enrollment application in progress")
		return
	}
	res, err := db.Exec("INSERT INTO enrollment_applications (student_id, program_id, status) VALUES (?,?, 'dean_review')", u.ID, in.ProgramID)
	if err != nil {
		writeErr(w, 500, "Could not submit enrollment")
		return
	}
	appID, _ := res.LastInsertId()
	db.Exec("INSERT INTO application_history (application_id,stage,action,actor_name,actor_role) VALUES (?,?,?,?,?)",
		appID, "submitted", "Enrollment application submitted", u.Name, u.Role)
	writeJSON(w, 200, map[string]interface{}{"id": appID, "status": "dean_review"})
}

func myEnrollmentHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	var appID int
	err := db.QueryRow("SELECT id FROM enrollment_applications WHERE student_id=? ORDER BY created_at DESC LIMIT 1", u.ID).Scan(&appID)
	if err != nil {
		writeJSON(w, 200, map[string]interface{}{"application": nil})
		return
	}
	data, err := getFullApplication(appID)
	if err != nil {
		writeErr(w, 500, "Could not load application")
		return
	}
	writeJSON(w, 200, map[string]interface{}{"application": data})
}

func resubmitHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	id := r.PathValue("id")
	var studentID int
	var status string
	if err := db.QueryRow("SELECT student_id,status FROM enrollment_applications WHERE id=?", id).Scan(&studentID, &status); err != nil || studentID != u.ID {
		writeErr(w, 404, "Application not found")
		return
	}
	var newStatus string
	switch status {
	case "dean_returned":
		newStatus = "dean_review"
	case "registrar_returned":
		newStatus = "registrar_review"
	default:
		writeErr(w, 400, "This application is not awaiting resubmission")
		return
	}
	db.Exec("UPDATE enrollment_applications SET status=?, return_reason=NULL WHERE id=?", newStatus, id)
	db.Exec("INSERT INTO application_history (application_id,stage,action,actor_name,actor_role) VALUES (?,?,?,?,?)",
		id, "resubmitted", "Application resubmitted by student", u.Name, u.Role)
	writeJSON(w, 200, map[string]string{"status": newStatus})
}

func uploadDocumentHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	id := r.PathValue("id")
	var studentID int
	if err := db.QueryRow("SELECT student_id FROM enrollment_applications WHERE id=?", id).Scan(&studentID); err != nil || studentID != u.ID {
		writeErr(w, 404, "Application not found")
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeErr(w, 400, "Could not read upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "No file provided")
		return
	}
	defer file.Close()
	dir := filepath.Join("uploads", id)
	os.MkdirAll(dir, 0755)
	safeName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(header.Filename))
	dest := filepath.Join(dir, safeName)
	out, err := os.Create(dest)
	if err != nil {
		writeErr(w, 500, "Could not save file")
		return
	}
	defer out.Close()
	io.Copy(out, file)
	db.Exec("INSERT INTO documents (application_id, original_name, stored_path) VALUES (?,?,?)", id, header.Filename, dest)
	db.Exec("INSERT INTO application_history (application_id,stage,action,actor_name,actor_role) VALUES (?,?,?,?,?)",
		id, "documents", "Uploaded document: "+header.Filename, u.Name, u.Role)
	writeJSON(w, 200, map[string]string{"status": "uploaded"})
}

// ---------- dean ----------

func queueHandler(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`
			SELECT a.id, u.name, p.name, a.created_at FROM enrollment_applications a
			JOIN users u ON u.id=a.student_id JOIN programs p ON p.id=a.program_id
			WHERE a.status=? ORDER BY a.created_at`, status)
		if err != nil {
			writeErr(w, 500, "Could not load queue")
			return
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var id int
			var sname, pname string
			var cat time.Time
			rows.Scan(&id, &sname, &pname, &cat)
			out = append(out, map[string]interface{}{"id": id, "student_name": sname, "program_name": pname, "created_at": cat.Format("Jan 2, 2006")})
		}
		writeJSON(w, 200, out)
	}
}

func deanSetSubjectsHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	id := r.PathValue("id")
	var in struct {
		SubjectIDs []int `json:"subject_ids"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "Invalid request body")
		return
	}
	tx, err := db.Begin()
	if err != nil {
		writeErr(w, 500, "Could not update subjects")
		return
	}
	tx.Exec("DELETE FROM application_subjects WHERE application_id=?", id)
	for _, sid := range in.SubjectIDs {
		tx.Exec("INSERT IGNORE INTO application_subjects (application_id,subject_id) VALUES (?,?)", id, sid)
	}
	tx.Commit()
	db.Exec("INSERT INTO application_history (application_id,stage,action,actor_name,actor_role) VALUES (?,?,?,?,?)",
		id, "dean", fmt.Sprintf("Suggested %d subject(s)", len(in.SubjectIDs)), u.Name, u.Role)
	writeJSON(w, 200, map[string]string{"status": "updated"})
}

func deanRemarksHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct{ Remarks string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "Invalid request body")
		return
	}
	db.Exec("UPDATE enrollment_applications SET dean_remarks=? WHERE id=?", in.Remarks, id)
	writeJSON(w, 200, map[string]string{"status": "updated"})
}

func deanApproveHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	id := r.PathValue("id")
	var count int
	db.QueryRow("SELECT COUNT(*) FROM application_subjects WHERE application_id=?", id).Scan(&count)
	if count == 0 {
		writeErr(w, 400, "Assign at least one subject before approving")
		return
	}
	db.Exec("UPDATE enrollment_applications SET status='registrar_review', dean_id=? WHERE id=?", u.ID, id)
	db.Exec("INSERT INTO application_history (application_id,stage,action,actor_name,actor_role) VALUES (?,?,?,?,?)",
		id, "dean", "Approved for enrollment", u.Name, u.Role)
	writeJSON(w, 200, map[string]string{"status": "registrar_review"})
}

func deanRejectHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	id := r.PathValue("id")
	var in struct{ Reason string }
	readJSON(r, &in)
	if in.Reason == "" {
		in.Reason = "Returned by the Dean for correction"
	}
	db.Exec("UPDATE enrollment_applications SET status='dean_returned', return_reason=? WHERE id=?", in.Reason, id)
	db.Exec("INSERT INTO application_history (application_id,stage,action,actor_name,actor_role,note) VALUES (?,?,?,?,?,?)",
		id, "dean", "Returned to student for correction", u.Name, u.Role, in.Reason)
	writeJSON(w, 200, map[string]string{"status": "dean_returned"})
}

// ---------- registrar ----------

func registrarVerifyHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	id := r.PathValue("id")
	var appID int
	fmt.Sscanf(id, "%d", &appID)
	var programID int
	db.QueryRow("SELECT program_id FROM enrollment_applications WHERE id=?", appID).Scan(&programID)
	var unitRate, miscFee float64
	db.QueryRow("SELECT unit_rate, misc_fee FROM programs WHERE id=?", programID).Scan(&unitRate, &miscFee)
	var totalUnits int
	db.QueryRow(`SELECT COALESCE(SUM(s.units),0) FROM application_subjects asub JOIN subjects s ON s.id=asub.subject_id WHERE asub.application_id=?`, appID).Scan(&totalUnits)
	if totalUnits == 0 {
		writeErr(w, 400, "No subjects have been assigned to this application yet")
		return
	}
	tuition := float64(totalUnits) * unitRate
	total := tuition + miscFee
	db.Exec(`INSERT INTO payments (application_id,total_units,tuition_fee,misc_fee,total_amount,status)
		VALUES (?,?,?,?,?, 'pending')
		ON DUPLICATE KEY UPDATE total_units=VALUES(total_units), tuition_fee=VALUES(tuition_fee), misc_fee=VALUES(misc_fee), total_amount=VALUES(total_amount), status='pending'`,
		appID, totalUnits, tuition, miscFee, total)
	db.Exec("UPDATE enrollment_applications SET status='for_payment', registrar_id=? WHERE id=?", u.ID, appID)
	db.Exec("INSERT INTO application_history (application_id,stage,action,actor_name,actor_role) VALUES (?,?,?,?,?)",
		appID, "registrar", "Verified and forwarded to Cashier", u.Name, u.Role)
	writeJSON(w, 200, map[string]string{"status": "for_payment"})
}

func registrarReturnHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	id := r.PathValue("id")
	var in struct{ Reason, Target string }
	readJSON(r, &in)
	if in.Reason == "" {
		in.Reason = "Returned by the Registrar for correction"
	}
	newStatus := "registrar_returned"
	label := "Returned to student for correction"
	if in.Target == "dean" {
		newStatus = "dean_review"
		label = "Returned to Dean for re-evaluation"
	}
	db.Exec("UPDATE enrollment_applications SET status=?, return_reason=? WHERE id=?", newStatus, in.Reason, id)
	db.Exec("INSERT INTO application_history (application_id,stage,action,actor_name,actor_role,note) VALUES (?,?,?,?,?,?)",
		id, "registrar", label, u.Name, u.Role, in.Reason)
	writeJSON(w, 200, map[string]string{"status": newStatus})
}

// ---------- cashier ----------

func cashierConfirmHandler(w http.ResponseWriter, r *http.Request) {
	u := fromCtx(r)
	id := r.PathValue("id")
	db.Exec("UPDATE payments SET status='confirmed', confirmed_at=NOW() WHERE application_id=?", id)
	db.Exec("UPDATE enrollment_applications SET status='enrolled', cashier_id=? WHERE id=?", u.ID, id)
	db.Exec("INSERT INTO application_history (application_id,stage,action,actor_name,actor_role) VALUES (?,?,?,?,?)",
		id, "cashier", "Payment confirmed — officially enrolled", u.Name, u.Role)
	writeJSON(w, 200, map[string]string{"status": "enrolled"})
}

// ---------- staff seeding ----------

func seedStaffAccounts() {
	staff := []struct{ name, email, role string }{
		{"Dr. Ana Reyes", "dean@school.edu", "dean"},
		{"Mr. Carlos Santos", "registrar@school.edu", "registrar"},
		{"Ms. Liza Torres", "cashier@school.edu", "cashier"},
	}
	hash, _ := hashPassword("password123")
	for _, s := range staff {
		_, err := db.Exec("INSERT INTO users (name,email,password_hash,role) VALUES (?,?,?,?)", s.name, s.email, hash, s.role)
		if err != nil {
			fmt.Printf("skipped %s (already exists?): %v\n", s.email, err)
		} else {
			fmt.Printf("created %s  /  password123\n", s.email)
		}
	}
}

// ---------- main ----------

func main() {
	seedStaff := flag.Bool("seed-staff", false, "create default dean/registrar/cashier accounts and exit")
	flag.Parse()

	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "root:password@tcp(127.0.0.1:3306)/enrollment_system?parseTime=true"
	}
	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		log.Fatal("could not connect to MySQL — check DB_DSN and that schema.sql has been loaded: ", err)
	}

	if *seedStaff {
		seedStaffAccounts()
		return
	}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/register", registerHandler)
	mux.HandleFunc("POST /api/login", loginHandler)
	mux.HandleFunc("GET /api/me", requireRole()(meHandler))
	mux.HandleFunc("PUT /api/me", requireRole("student")(updateProfileHandler))

	mux.HandleFunc("GET /api/programs", requireRole()(programsHandler))
	mux.HandleFunc("GET /api/programs/{id}/subjects", requireRole()(subjectsByProgramHandler))

	mux.HandleFunc("POST /api/enrollment", requireRole("student")(submitEnrollmentHandler))
	mux.HandleFunc("GET /api/enrollment/mine", requireRole("student")(myEnrollmentHandler))
	mux.HandleFunc("POST /api/enrollment/{id}/resubmit", requireRole("student")(resubmitHandler))
	mux.HandleFunc("POST /api/enrollment/{id}/documents", requireRole("student")(uploadDocumentHandler))
	mux.HandleFunc("GET /api/enrollment/{id}", requireRole()(applicationDetailHandler))

	mux.HandleFunc("GET /api/dean/queue", requireRole("dean")(queueHandler("dean_review")))
	mux.HandleFunc("POST /api/dean/applications/{id}/subjects", requireRole("dean")(deanSetSubjectsHandler))
	mux.HandleFunc("POST /api/dean/applications/{id}/remarks", requireRole("dean")(deanRemarksHandler))
	mux.HandleFunc("POST /api/dean/applications/{id}/approve", requireRole("dean")(deanApproveHandler))
	mux.HandleFunc("POST /api/dean/applications/{id}/reject", requireRole("dean")(deanRejectHandler))

	mux.HandleFunc("GET /api/registrar/queue", requireRole("registrar")(queueHandler("registrar_review")))
	mux.HandleFunc("POST /api/registrar/applications/{id}/verify", requireRole("registrar")(registrarVerifyHandler))
	mux.HandleFunc("POST /api/registrar/applications/{id}/return", requireRole("registrar")(registrarReturnHandler))

	mux.HandleFunc("GET /api/cashier/queue", requireRole("cashier")(queueHandler("for_payment")))
	mux.HandleFunc("POST /api/cashier/applications/{id}/confirm", requireRole("cashier")(cashierConfirmHandler))

	mux.Handle("/", http.FileServer(http.Dir("static")))

	log.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
