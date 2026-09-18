# Contributing

Thank you for contributing to the Carbon Tracker Windows Launcher.

## Development setup

1. Install Go 1.23+ and Python 3.10+.
2. Fork/clone the repository.
3. Create a feature branch from `main`.
4. Run `./build.ps1` on Windows or use the GitHub Actions build workflow.
5. Test on Windows 10/11 with Docker Desktop.

## Pull requests

Please keep changes focused and include:

- what changed and why;
- Windows version tested;
- Docker Desktop version tested, if relevant;
- the exact test sequence used (for example Start → Stop → Start);
- screenshots for UI changes;
- confirmation that fresh installation and existing-installation flows are not unintentionally mixed.

Never commit secrets, signing keys, generated `.env` files, or user data.
