# Suggested commit sequence

Keeping the initial publication in focused commits makes the history easier to audit.

1. `chore: add Carbon Tracker Windows launcher source structure`
2. `feat: add Docker Desktop and Carbon Tracker install/update workflows`
3. `fix: stabilize Docker Compose detection and credential helper handling`
4. `ui: add Carbon Tracker launcher styling and progress feedback`
5. `build: embed Carbon Tracker icon in Windows executable`
6. `ci: add reproducible Windows launcher build workflow`
7. `docs: add usage, security, publishing and code-signing guides`
8. `legal: add <CHOSEN-LICENSE> license matching Sustainability Monitoring Hub`
9. `release: prepare v4.4.0`

For future releases, keep functional changes, build/signing changes and release notes in separate commits where practical.
