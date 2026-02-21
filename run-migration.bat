@echo off
cd /d "%~dp0src"
set DATABASE_URL=postgresql://postgres:recep@localhost:5432/servertest_local
go run ./cmd/migrate/main.go
pause
