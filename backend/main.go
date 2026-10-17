package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

var db *sql.DB

func main() {
	cfg := mysql.Config{User: env("DB_USER", "root"), Passwd: os.Getenv("DB_PASSWORD"), Net: "tcp", Addr: env("DB_ADDR", "127.0.0.1:3306"), DBName: env("DB_NAME", "project_management"), ParseTime: true, Loc: time.UTC}
	var err error
	db, err = sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	if err = db.Ping(); err != nil {
		log.Fatalf("MySQL connection failed: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", health)
	mux.HandleFunc("/api/projects", projects)
	mux.HandleFunc("/api/projects/", projectSubroutes)
	mux.HandleFunc("/api/team-members", members)
	mux.HandleFunc("/api/team-members/", memberByID)
	mux.HandleFunc("/api/project-members", assignments)
	mux.HandleFunc("/api/project-tasks", tasks)
	mux.HandleFunc("/api/project-tasks/", taskByID)
	addr := env("API_ADDR", ":8080")
	log.Printf("MySQL connected; API listening at http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, cors(mux)))
}
func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", env("FRONTEND_ORIGIN", "http://localhost:4200"))
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"error": msg})
}
func body(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	return nil
}
func method(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	fail(w, 405, "Method not allowed")
	return false
}
func health(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	if err := db.Ping(); err != nil {
		fail(w, 503, "Database unavailable")
		return
	}
	reply(w, 200, map[string]string{"message": "Backend is working!", "database": "connected"})
}
func page(r *http.Request) (int, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if p < 1 {
		p = 1
	}
	if n < 1 {
		n = 20
	}
	if n > 100 {
		n = 100
	}
	return p, n
}
func parseDate(s string) (string, error) {
	t, e := time.Parse("2006-01-02", s)
	if e != nil {
		return "", errors.New("dates must use YYYY-MM-DD format")
	}
	return t.Format("2006-01-02"), nil
}

type Project struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Status      string `json:"status"`
	Progress    int    `json:"progress"`
}
type ProjectInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Status      string `json:"status"`
}

func validateProject(x *ProjectInput) error {
	x.Name = strings.TrimSpace(x.Name)
	if x.Name == "" {
		return errors.New("name is required")
	}
	a, e := parseDate(x.StartDate)
	if e != nil {
		return e
	}
	b, e := parseDate(x.EndDate)
	if e != nil {
		return e
	}
	if b < a {
		return errors.New("end_date must be on or after start_date")
	}
	x.StartDate = a
	x.EndDate = b
	if x.Status == "" {
		x.Status = "Active"
	}
	if x.Status != "Active" && x.Status != "Completed" && x.Status != "On Hold" {
		return errors.New("status must be Active, Completed, or On Hold")
	}
	return nil
}
func projects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		listProjects(w, r)
	case "POST":
		createProject(w, r)
	case "PUT":
		updateProject(w, r)
	case "DELETE":
		deleteProject(w, r)
	default:
		method(w, r, "GET", "POST", "PUT", "DELETE")
	}
}
func listProjects(w http.ResponseWriter, r *http.Request) {
	p, n := page(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	rows, e := db.Query(`SELECT p.id,p.name,COALESCE(p.description,''),DATE_FORMAT(p.start_date,'%Y-%m-%d'),DATE_FORMAT(p.end_date,'%Y-%m-%d'),p.status,COALESCE(ROUND(100*SUM(t.status='Completed')/NULLIF(COUNT(t.id),0)),0) FROM projects p LEFT JOIN project_tasks t ON t.project_id=p.id WHERE (?='' OR p.name LIKE CONCAT('%',?,'%') OR p.description LIKE CONCAT('%',?,'%')) AND (?='' OR p.status=?) GROUP BY p.id ORDER BY p.id DESC LIMIT ? OFFSET ?`, q, q, q, status, status, n, (p-1)*n)
	if e != nil {
		fail(w, 500, "Could not load projects")
		return
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var x Project
		if e = rows.Scan(&x.ID, &x.Name, &x.Description, &x.StartDate, &x.EndDate, &x.Status, &x.Progress); e != nil {
			fail(w, 500, "Could not read projects")
			return
		}
		out = append(out, x)
	}
	reply(w, 200, map[string]any{"items": out, "page": p, "limit": n})
}
func createProject(w http.ResponseWriter, r *http.Request) {
	var x ProjectInput
	if body(r, &x) != nil {
		fail(w, 400, "Invalid JSON body")
		return
	}
	if e := validateProject(&x); e != nil {
		fail(w, 400, e.Error())
		return
	}
	res, e := db.Exec(`INSERT INTO projects(name,description,start_date,end_date,status) VALUES(?,?,?,?,?)`, x.Name, x.Description, x.StartDate, x.EndDate, x.Status)
	if e != nil {
		fail(w, 500, "Could not create project")
		return
	}
	id, _ := res.LastInsertId()
	reply(w, 201, map[string]any{"id": id, "message": "Project created successfully"})
}
func updateProject(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		fail(w, 400, "Project id is required")
		return
	}
	var x ProjectInput
	if body(r, &x) != nil {
		fail(w, 400, "Invalid JSON body")
		return
	}
	if e := validateProject(&x); e != nil {
		fail(w, 400, e.Error())
		return
	}
	res, e := db.Exec(`UPDATE projects SET name=?,description=?,start_date=?,end_date=?,status=? WHERE id=?`, x.Name, x.Description, x.StartDate, x.EndDate, x.Status, id)
	if e != nil {
		fail(w, 500, "Could not update project")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists int
		e = db.QueryRow(`SELECT id FROM projects WHERE id=?`, id).Scan(&exists)
		if e == sql.ErrNoRows {
			fail(w, 404, "Project not found")
			return
		}
	}
	reply(w, 200, map[string]any{"id": id, "message": "Project updated successfully"})
}
func deleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		fail(w, 400, "Project id is required")
		return
	}
	res, e := db.Exec(`DELETE FROM projects WHERE id=?`, id)
	if e != nil {
		fail(w, 409, "Could not delete project; remove its tasks and assignments first")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "Project not found")
		return
	}
	reply(w, 200, map[string]string{"message": "Project deleted successfully"})
}
func projectSubroutes(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "tasks" {
		tasksForProject(w, r, parts[2])
		return
	}
	fail(w, 404, "Route not found")
}
func tasksForProject(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		method(w, r, "GET")
		return
	}
	q := r.URL.Query()
	q.Set("project_id", id)
	r.URL.RawQuery = q.Encode()
	listTasks(w, r)
}

type Member struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func members(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		p, n := page(r)
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		rows, e := db.Query(`SELECT id,name,email,COALESCE(role,'') FROM team_members WHERE (?='' OR name LIKE CONCAT('%',?,'%') OR email LIKE CONCAT('%',?,'%')) ORDER BY id DESC LIMIT ? OFFSET ?`, q, q, q, n, (p-1)*n)
		if e != nil {
			fail(w, 500, "Could not load team members")
			return
		}
		defer rows.Close()
		out := []Member{}
		for rows.Next() {
			var x Member
			if rows.Scan(&x.ID, &x.Name, &x.Email, &x.Role) != nil {
				fail(w, 500, "Could not read team members")
				return
			}
			out = append(out, x)
		}
		reply(w, 200, map[string]any{"items": out, "page": p, "limit": n})
	case "POST":
		var x Member
		if body(r, &x) != nil {
			fail(w, 400, "Invalid JSON body")
			return
		}
		x.Name = strings.TrimSpace(x.Name)
		x.Email = strings.TrimSpace(x.Email)
		if x.Name == "" || x.Email == "" {
			fail(w, 400, "name and email are required")
			return
		}
		res, e := db.Exec(`INSERT INTO team_members(name,email,role) VALUES(?,?,?)`, x.Name, x.Email, x.Role)
		if e != nil {
			fail(w, 409, "Could not create member; email may already exist")
			return
		}
		id, _ := res.LastInsertId()
		reply(w, 201, map[string]any{"id": id, "message": "Team member created"})
	default:
		method(w, r, "GET", "POST")
	}
}
func memberByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/team-members/")
	switch r.Method {
	case "DELETE":
		res, e := db.Exec(`DELETE FROM team_members WHERE id=?`, id)
		if e != nil {
			fail(w, 409, "Could not delete member; remove project assignments first")
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			fail(w, 404, "Team member not found")
			return
		}
		reply(w, 200, map[string]string{"message": "Team member deleted"})
	case "PUT":
		var x Member
		if body(r, &x) != nil {
			fail(w, 400, "Invalid JSON body")
			return
		}
		x.Name = strings.TrimSpace(x.Name)
		x.Email = strings.TrimSpace(x.Email)
		if x.Name == "" || x.Email == "" {
			fail(w, 400, "name and email are required")
			return
		}
		res, e := db.Exec(`UPDATE team_members SET name=?,email=?,role=? WHERE id=?`, x.Name, x.Email, x.Role, id)
		if e != nil {
			fail(w, 409, "Could not update member; email may already be in use")
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			var found int
			if db.QueryRow(`SELECT id FROM team_members WHERE id=?`, id).Scan(&found) == sql.ErrNoRows {
				fail(w, 404, "Team member not found")
				return
			}
		}
		reply(w, 200, map[string]string{"message": "Team member updated"})
	default:
		method(w, r, "PUT", "DELETE")
	}
}
func assignments(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		var x struct {
			ProjectID int `json:"project_id"`
			MemberID  int `json:"member_id"`
		}
		if body(r, &x) != nil || x.ProjectID < 1 || x.MemberID < 1 {
			fail(w, 400, "Valid project_id and member_id are required")
			return
		}
		res, e := db.Exec(`DELETE FROM project_members WHERE project_id=? AND member_id=?`, x.ProjectID, x.MemberID)
		if e != nil {
			fail(w, 500, "Could not remove assignment")
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			fail(w, 404, "Assignment not found")
			return
		}
		reply(w, 200, map[string]string{"message": "Assignment removed"})
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		method(w, r, "GET", "POST", "DELETE")
		return
	}
	if r.Method == http.MethodGet {
		rows, e := db.Query(`SELECT pm.project_id,pm.member_id,p.name,m.name FROM project_members pm JOIN projects p ON p.id=pm.project_id JOIN team_members m ON m.id=pm.member_id ORDER BY p.name,m.name`)
		if e != nil {
			fail(w, 500, "Could not load assignments")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var p, m int
			var pn, mn string
			if rows.Scan(&p, &m, &pn, &mn) != nil {
				fail(w, 500, "Could not read assignments")
				return
			}
			out = append(out, map[string]any{"project_id": p, "member_id": m, "project_name": pn, "member_name": mn})
		}
		reply(w, 200, out)
		return
	}
	var x struct {
		ProjectID int `json:"project_id"`
		MemberID  int `json:"member_id"`
	}
	if body(r, &x) != nil || x.ProjectID < 1 || x.MemberID < 1 {
		fail(w, 400, "Valid project_id and member_id are required")
		return
	}
	_, e := db.Exec(`INSERT INTO project_members(project_id,member_id) VALUES(?,?)`, x.ProjectID, x.MemberID)
	if e != nil {
		fail(w, 409, "Could not assign member; check IDs and duplicate assignment")
		return
	}
	reply(w, 201, map[string]string{"message": "Member assigned to project"})
}

type Task struct {
	ID           int    `json:"id"`
	ProjectID    int    `json:"project_id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	AssignedTo   *int   `json:"assigned_to"`
	AssignedName string `json:"assigned_name"`
	DueDate      string `json:"due_date"`
	Status       string `json:"status"`
}
type TaskInput struct {
	ProjectID   int    `json:"project_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	AssignedTo  *int   `json:"assigned_to"`
	DueDate     string `json:"due_date"`
	Status      string `json:"status"`
}

func tasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		listTasks(w, r)
	case "POST":
		createTask(w, r)
	default:
		method(w, r, "GET", "POST")
	}
}
func listTasks(w http.ResponseWriter, r *http.Request) {
	p, n := page(r)
	pid := r.URL.Query().Get("project_id")
	rows, e := db.Query(`SELECT t.id,t.project_id,t.title,COALESCE(t.description,''),t.assigned_to,COALESCE(m.name,''),COALESCE(DATE_FORMAT(t.due_date,'%Y-%m-%d'),''),t.status FROM project_tasks t LEFT JOIN team_members m ON m.id=t.assigned_to WHERE (?='' OR t.project_id=?) ORDER BY t.id DESC LIMIT ? OFFSET ?`, pid, pid, n, (p-1)*n)
	if e != nil {
		fail(w, 500, "Could not load tasks")
		return
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		var x Task
		if rows.Scan(&x.ID, &x.ProjectID, &x.Title, &x.Description, &x.AssignedTo, &x.AssignedName, &x.DueDate, &x.Status) != nil {
			fail(w, 500, "Could not read tasks")
			return
		}
		out = append(out, x)
	}
	reply(w, 200, map[string]any{"items": out, "page": p, "limit": n})
}
func validateTask(x *TaskInput) error {
	x.Title = strings.TrimSpace(x.Title)
	if x.ProjectID < 1 || x.Title == "" {
		return errors.New("project_id and title are required")
	}
	if x.Status == "" {
		x.Status = "Pending"
	}
	if x.Status != "Pending" && x.Status != "In Progress" && x.Status != "Completed" {
		return errors.New("status must be Pending, In Progress, or Completed")
	}
	if x.DueDate != "" {
		d, e := parseDate(x.DueDate)
		if e != nil {
			return e
		}
		x.DueDate = d
	}
	return nil
}
func createTask(w http.ResponseWriter, r *http.Request) {
	var x TaskInput
	if body(r, &x) != nil {
		fail(w, 400, "Invalid JSON body")
		return
	}
	if e := validateTask(&x); e != nil {
		fail(w, 400, e.Error())
		return
	}
	var status string
	e := db.QueryRow(`SELECT status FROM projects WHERE id=?`, x.ProjectID).Scan(&status)
	if e == sql.ErrNoRows {
		fail(w, 404, "Project not found")
		return
	}
	if e != nil {
		fail(w, 500, "Could not check project")
		return
	}
	if status != "Active" {
		fail(w, 409, "Tasks can only be added to active projects")
		return
	}
	res, e := db.Exec(`INSERT INTO project_tasks(project_id,title,description,assigned_to,due_date,status) VALUES(?,?,?,?,NULLIF(?,''),?)`, x.ProjectID, x.Title, x.Description, x.AssignedTo, x.DueDate, x.Status)
	if e != nil {
		fail(w, 409, "Could not create task; check assigned member")
		return
	}
	id, _ := res.LastInsertId()
	reply(w, 201, map[string]any{"id": id, "message": "Task created"})
}
func taskByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/project-tasks/")
	switch r.Method {
	case "PUT":
		updateTask(w, r, id)
	case "DELETE":
		res, e := db.Exec(`DELETE FROM project_tasks WHERE id=?`, id)
		if e != nil {
			fail(w, 500, "Could not delete task")
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			fail(w, 404, "Task not found")
			return
		}
		reply(w, 200, map[string]string{"message": "Task deleted"})
	default:
		method(w, r, "PUT", "DELETE")
	}
}
func updateTask(w http.ResponseWriter, r *http.Request, id string) {
	var x TaskInput
	if body(r, &x) != nil {
		fail(w, 400, "Invalid JSON body")
		return
	}
	if x.ProjectID < 1 {
		var cur int
		if db.QueryRow(`SELECT project_id FROM project_tasks WHERE id=?`, id).Scan(&cur) != nil {
			fail(w, 404, "Task not found")
			return
		}
		x.ProjectID = cur
	}
	if e := validateTask(&x); e != nil {
		fail(w, 400, e.Error())
		return
	}
	var status string
	if e := db.QueryRow(`SELECT status FROM projects WHERE id=?`, x.ProjectID).Scan(&status); e != nil {
		fail(w, 404, "Project not found")
		return
	}
	if status != "Active" {
		fail(w, 409, "Tasks can only be updated on active projects")
		return
	}
	res, e := db.Exec(`UPDATE project_tasks SET project_id=?,title=?,description=?,assigned_to=?,due_date=NULLIF(?,''),status=? WHERE id=?`, x.ProjectID, x.Title, x.Description, x.AssignedTo, x.DueDate, x.Status, id)
	if e != nil {
		fail(w, 409, "Could not update task; check assigned member")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var found int
		if db.QueryRow(`SELECT id FROM project_tasks WHERE id=?`, id).Scan(&found) == sql.ErrNoRows {
			fail(w, 404, "Task not found")
			return
		}
	}
	reply(w, 200, map[string]string{"message": "Task updated"})
}
