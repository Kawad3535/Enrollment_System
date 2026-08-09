# Registrar's Online Enrollment System

HTML/CSS/JS frontend, Go backend, MySQL database. Implements the full flow:

Student submits → Dean evaluates & assigns subjects → Registrar verifies →
Cashier confirms payment → Enrolled → Enrollment slip.

## 1. Set up the database

```bash
mysql -u root -p < schema.sql
```

This creates the `enrollment_system` database, all tables, the three programs
(Degree / Masteral / TCP), and a sample subject bank per program.

## 2. Configure the connection

The server reads its MySQL connection string from the `DB_DSN` environment
variable. Default (if unset):

```
root:password@tcp(127.0.0.1:3306)/enrollment_system?parseTime=true
```

Set your own, e.g.:

```bash
export DB_DSN="root:yourpassword@tcp(127.0.0.1:3306)/enrollment_system?parseTime=true"
```

## 3. Install dependencies and seed staff accounts

```bash
go mod tidy
go run . --seed-staff
```

This creates three staff logins (dean, registrar, cashier), all with
password `password123`:

- dean@school.edu
- registrar@school.edu
- cashier@school.edu

Students register themselves from the app — there's a "Create student
account" tab on the sign-in screen.

## 4. Run the server

```bash
go run .
```

Then open **http://localhost:8080**.

## How the modules map to the code

| Module    | Frontend            | Backend routes                                  |
|-----------|----------------------|--------------------------------------------------|
| Student   | `renderStudent`, `renderProfileAndPicker`, `renderStudentStatus`, `renderSlip` in `app.js` | `/api/register`, `/api/me`, `/api/programs`, `/api/enrollment*` |
| Dean      | `renderDeanDetail`    | `/api/dean/*` |
| Registrar | `renderRegistrarDetail` | `/api/registrar/*` |
| Cashier   | `renderCashierDetail` | `/api/cashier/*` |

Status flow (stored in `enrollment_applications.status`):

```
dean_review → registrar_review → for_payment → enrolled
     ↑ (dean_returned)   ↑ (registrar_returned)
```

Returned applications go back to the student, who can revise and resubmit
(`POST /api/enrollment/{id}/resubmit`), which routes them back to whichever
office returned them.

## Notes / things to harden before real deployment

- Passwords are hashed with salted SHA-256 for simplicity, with no external
  dependency required. Swap in `golang.org/x/crypto/bcrypt` for production.
- Sessions are an in-memory token map — they reset if the server restarts,
  and won't work across multiple server instances. Move to a `sessions`
  table or JWT for anything beyond a single-process demo.
- Uploaded documents are written to `./uploads/{application_id}/` on local
  disk. Point this at object storage (S3, GCS, etc.) for production.
- Tuition rates are hardcoded per program in the `programs` table
  (`unit_rate`, `misc_fee`) — adjust there.
- `applicationDetailHandler` currently lets any authenticated user fetch any
  application by ID. For production, add an ownership/role check so a
  student can only fetch their own record.
