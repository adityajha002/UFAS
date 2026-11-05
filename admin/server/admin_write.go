package main

// Write routes: faculty management, manual attendance, shutdown. Thin wrappers
// over the same functions the CLI uses (db.go); validation is stricter here
// than in the CLI and applies to HTTP only.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// cleanText trims s and checks length (in characters) and control characters.
// problem is "" when the text is fine.
func cleanText(s string, max int) (clean, problem string) {
	s = strings.TrimSpace(s)
	switch n := utf8.RuneCountInString(s); {
	case n == 0:
		return "", "required"
	case n > max:
		return "", fmt.Sprintf("must be at most %d characters", max)
	case !utf8.ValidString(s):
		return "", "must be valid UTF-8"
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return "", "must not contain control characters"
		}
	}
	return s, ""
}

// cleanEmployeeID is cleanText plus: it is used inside URL paths.
func cleanEmployeeID(s string) (clean, problem string) {
	clean, problem = cleanText(s, 64)
	if problem == "" && strings.ContainsAny(clean, `/\`) {
		return "", `must not contain / or \`
	}
	return clean, problem
}

func invalidInput(fields map[string]string) error {
	return adminErr{code: "invalid_input", msg: fmt.Sprintf("%d field(s) invalid", len(fields)), fields: fields}
}

func noSuchEmployee() error { return adminErr{code: "not_found", msg: "no such employee"} }

// POST /admin/api/faculty   {"employee_id","name"}
func (s *Server) adminFacultyAdd(w http.ResponseWriter, r *http.Request) (status int, body any, err error) {
	var target string
	var detail any
	defer s.trackAction(r, "faculty_add", &target, &detail, &err)()

	var req struct {
		EmployeeID string `json:"employee_id"`
		Name       string `json:"name"`
	}
	if err := decode(w, r, &req); err != nil {
		return 0, nil, err
	}
	fields := map[string]string{}
	emp, p1 := cleanEmployeeID(req.EmployeeID)
	if p1 != "" {
		fields["employee_id"] = p1
	}
	name, p2 := cleanText(req.Name, 100)
	if p2 != "" {
		fields["name"] = p2
	}
	if len(fields) > 0 {
		return 0, nil, invalidInput(fields)
	}
	target, detail = emp, map[string]string{"name": name}
	if _, err := addFaculty(s.db, emp, name); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, nil, adminErr{code: "already_exists"}
		}
		return 0, nil, err
	}
	return 201, map[string]any{"employee_id": emp, "name": name, "enrolled": false}, nil
}

// POST /admin/api/faculty/{employee_id}/code
// The code is returned here and nowhere else: not logged, not in any table.
func (s *Server) adminCodeIssue(w http.ResponseWriter, r *http.Request) (status int, body any, err error) {
	emp := r.PathValue("employee_id")
	target := emp
	var detail any
	defer s.trackAction(r, "code_issue", &target, &detail, &err)()

	now := s.now()
	code, ierr := issueCode(s.db, emp, now)
	if errors.Is(ierr, errNotFound) {
		return 0, nil, noSuchEmployee()
	}
	if ierr != nil {
		return 0, nil, ierr
	}
	return 201, map[string]any{
		"employee_id":    emp,
		"code":           code,
		"expires_in_s":   int(codeTTL.Seconds()),
		"expires_at_ist": now.Add(codeTTL).In(ist).Format(istLayout),
	}, nil
}

// POST /admin/api/faculty/{employee_id}/revoke
func (s *Server) adminFacultyRevoke(w http.ResponseWriter, r *http.Request) (status int, body any, err error) {
	emp := r.PathValue("employee_id")
	target := emp
	var detail any
	defer s.trackAction(r, "faculty_revoke", &target, &detail, &err)()

	if rerr := revokeFaculty(s.db, emp); errors.Is(rerr, errNotFound) {
		return 0, nil, noSuchEmployee()
	} else if rerr != nil {
		return 0, nil, rerr
	}
	return 200, map[string]any{"employee_id": emp, "enrolled": false}, nil
}

// DELETE /admin/api/faculty/{employee_id}
func (s *Server) adminFacultyDelete(w http.ResponseWriter, r *http.Request) (status int, body any, err error) {
	emp := r.PathValue("employee_id")
	target := emp
	var detail any
	defer s.trackAction(r, "faculty_delete", &target, &detail, &err)()

	if derr := deleteFaculty(s.db, emp); errors.Is(derr, errNotFound) {
		return 0, nil, noSuchEmployee()
	} else if derr != nil {
		return 0, nil, derr
	}
	return 200, map[string]any{"employee_id": emp, "deleted": true}, nil
}

// POST /admin/api/attendance/manual   {"employee_id","date","note"}
func (s *Server) adminManualMark(w http.ResponseWriter, r *http.Request) (status int, body any, err error) {
	var target string
	var detail any
	defer s.trackAction(r, "manual_mark", &target, &detail, &err)()

	var req struct {
		EmployeeID string `json:"employee_id"`
		Date       string `json:"date"`
		Note       string `json:"note"`
	}
	if err := decode(w, r, &req); err != nil {
		return 0, nil, err
	}
	fields := map[string]string{}
	emp, p1 := cleanEmployeeID(req.EmployeeID)
	if p1 != "" {
		fields["employee_id"] = p1
	}
	note, p2 := cleanText(req.Note, 200) // a reason is required so manual entries are auditable
	if p2 != "" {
		fields["note"] = p2
	}
	if strings.TrimSpace(req.Date) == "" {
		fields["date"] = "required"
	}
	if len(fields) > 0 {
		return 0, nil, invalidInput(fields)
	}
	day, derr := parseDay(strings.TrimSpace(req.Date))
	if derr != nil {
		return 0, nil, derr // bad_date
	}
	now := s.now()
	if day.After(todayIST(now)) {
		return 0, nil, adminErr{code: "future_date"}
	}
	target, detail = emp, map[string]string{"date": ymd(day), "note": note}
	switch merr := markManual(s.db, emp, ymd(day), note, now); {
	case merr == nil:
	case errors.Is(merr, errNotFound):
		return 0, nil, noSuchEmployee()
	case errors.Is(merr, errAlreadyMarked):
		return 0, nil, adminErr{code: "already_marked"}
	default:
		return 0, nil, merr
	}
	return 201, map[string]any{"employee_id": emp, "date": ymd(day), "source": "manual"}, nil
}

// POST /admin/api/server/shutdown   {"confirm":"shutdown"}
// The admin panel stays alive; it only forwards the shutdown request to the real
// phone-facing server so the UI can remain available to restart it.
func (s *Server) adminShutdown(w http.ResponseWriter, r *http.Request) (int, any, error) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := decode(w, r, &req); err != nil || req.Confirm != "shutdown" {
		s.recordAction(r, "shutdown", "", nil, "confirm_required")
		return 0, nil, adminErr{code: "confirm_required"}
	}
	s.recordAction(r, "shutdown", "", nil, "ok")
	if err := s.forwardMainServerShutdown(); err != nil {
		log.Printf("main server shutdown forward failed: %v", err)
	}
	return 202, map[string]string{"status": "shutting_down", "at_ist": s.now().In(ist).Format(istLayout)}, nil
}

func (s *Server) forwardMainServerShutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	payload := strings.NewReader(`{"confirm":"shutdown"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mainServerShutdownURL+"/server/shutdown", payload)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
