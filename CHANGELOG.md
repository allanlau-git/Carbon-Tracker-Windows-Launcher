# Changelog

All notable changes to the Windows launcher are documented here.

## 4.4.0 - 2026-08-08

### Added
- Separate Docker Desktop install/update action.
- First-time Carbon Tracker installation flow.
- Existing-installation update checking against the upstream Git commit.
- Start, Stop, Open Carbon Tracker, View Logs, and Open Folder controls.
- Bold progress status and “Please wait....” feedback for long operations.
- Asia/Kuala_Lumpur timezone for fresh local deployments.
- Docker Desktop path and Docker Compose compatibility handling.
- Embedded Windows application icon resource.

### Fixed
- Docker credential-helper resolution after fresh Docker Desktop installation.
- Docker Compose invocation compatibility (`docker compose` / `docker-compose.exe`).
- UI thread hangs caused by blocking external operations.
- Compose path detection for existing installations.
