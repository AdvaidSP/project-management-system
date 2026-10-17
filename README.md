# TeamFlow — Project and Team Management System

A beginner-friendly full-stack project for managing projects, team members, assignments, and tasks. The backend uses Go's standard HTTP library, the MySQL driver, and parameterized SQL. The frontend is an Angular single-page app.

## Included features

- Projects: create, list, update, delete, search, filter by status, and paginate.
- Team: create, list, update, delete, search, and paginate members.
- Assignments: list, add, and remove project/member links. The database primary key prevents duplicate assignment.
- Tasks: create, list, update status/details, delete, filter by project, and paginate.
- Validation: project dates use `YYYY-MM-DD`, end date cannot precede start date, task creation/update requires an active project, and progress is calculated from completed tasks.
- Overview dashboard with counts and progress bars.

## Requirements

- MySQL 8 or a recent MySQL version with InnoDB and CHECK constraints
- Go 1.22 or newer
- Node.js 20.11 or newer and npm

## 1. Create the database

Open a MySQL client (MySQL Workbench or command prompt) and run `database/schema.sql`. It creates the `project_management` database and the four tables. It adds a sample project only if there are no projects yet.

If you already created these tables, back up your database first. This schema documents the expected columns and constraints; it does not alter an existing table definition.

## 2. Start the Go API (Windows)

Open Command Prompt in `backend` and run:

```bat
set DB_USER=root
set DB_PASSWORD=your_mysql_password
set DB_ADDR=127.0.0.1:3306
set DB_NAME=project_management
go mod tidy
go run .
```

Use the password you set for your local MySQL account. The API listens on `http://localhost:8080`. Keep this window open while using the frontend. If port 8080 is busy, stop the other backend process or run `set API_ADDR=:8081` and update the frontend API URL in `src/app/app.component.ts`.

Check the server in a browser at `http://localhost:8080/api/health`. A successful response includes `"database":"connected"`.

## 3. Start Angular

Open a second Command Prompt in `frontend` and run:

```bat
npm install
npm start
```

Visit `http://localhost:4200`. The API allows requests from this address by default. Create an active project before creating tasks. The dashboard's progress is the share of tasks marked Completed.

## API quick reference

All request and response bodies use JSON. List endpoints accept `page` and `limit` (default 1 and 20; maximum 100). Project and member lists accept `q` search. Project list accepts `status`. Task list accepts `project_id`.

| Method | Endpoint | Purpose |
|---|---|---|
| GET | `/api/health` | API and database check |
| GET, POST, PUT, DELETE | `/api/projects` | Project CRUD; PUT/DELETE use `?id=1` |
| GET | `/api/projects/{id}/tasks` | Tasks for one project |
| GET, POST | `/api/team-members` | List or create member |
| PUT, DELETE | `/api/team-members/{id}` | Update or remove member |
| GET, POST, DELETE | `/api/project-members` | List/add/remove assignments; DELETE body has both IDs |
| GET, POST | `/api/project-tasks` | List or create task |
| PUT, DELETE | `/api/project-tasks/{id}` | Update or remove task |

Example project JSON:

```json
{"name":"Website Development","description":"Build the company website","start_date":"2026-10-20","end_date":"2026-12-25","status":"Active"}
```

Example task JSON:

```json
{"project_id":1,"title":"Prepare wireframes","description":"Draft initial screens","assigned_to":1,"due_date":"2026-11-05","status":"Pending"}
```

Supported project statuses: `Active`, `On Hold`, `Completed`. Task statuses: `Pending`, `In Progress`, `Completed`. Deleting a project cascades to its tasks and assignments. Deleting a member is restricted while the member still has project assignments; remove those assignments first. Existing tasks assigned to a deleted member become unassigned.

## Troubleshooting

- **Database connection failed:** ensure MySQL is running, the database exists, and the user/password are correct.
- **Port already in use:** only one API process can listen on port 8080. Close the other terminal running `go run .`.
- **Frontend cannot connect:** check the backend health URL and keep the backend process open. Check `FRONTEND_ORIGIN` if Angular is running on a different port.
- **Foreign key error when deleting a member:** remove the member's project assignments first.

## Project structure

```text
backend/       Go REST API
database/      MySQL schema and sample row
frontend/      Angular user interface
```
