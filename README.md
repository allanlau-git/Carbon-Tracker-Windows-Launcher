# Carbon Tracker Windows Launcher

A native Windows GUI installer and launcher for the **Sustainability Monitoring Hub / Carbon Tracker**.

The launcher is designed for users who want to run the Sustainability Monitoring Hub locally without manually setting up Python, MySQL, Docker Compose files, or command-line scripts.

## What it does

- Installs or updates Docker Desktop separately from Carbon Tracker.
- Performs a first-time local Carbon Tracker installation from the upstream GitHub repository.
- Checks for upstream Carbon Tracker updates before rebuilding.
- Starts and stops the existing Docker application without reinstalling it.
- Opens Carbon Tracker at `http://localhost:8501`.
- Exports Docker logs for troubleshooting.
- Opens the local installation folder.
- Uses `Asia/Kuala_Lumpur` (`UTC+08:00`) for fresh installations.
- Preserves Docker database volumes and local application data during normal updates.
- Embeds the Carbon Tracker leaf icon directly in the Windows executable.
- Runs as a native Windows GUI application; no CMD or PowerShell launcher is required for normal use.

## Upstream application

This launcher installs and manages:

`https://github.com/msf4-0/Sustainability-Monitoring-Hub-202602`

The launcher itself does not replace or fork the Sustainability Monitoring Hub application. It automates local Windows deployment of the upstream project.

## Supported platform

- Windows 10 or Windows 11, x64
- WSL 2 capable Windows installation for Docker Desktop
- Internet connection for initial Docker Desktop / Carbon Tracker downloads

## User workflow

### New computer

1. Run `CarbonTrackerLauncher-v4.4.0.exe`.
2. Click **Docker Desktop**.
3. Wait for Docker Desktop installation/update to finish.
4. Start Docker Desktop and wait until Docker reports that it is running.
5. Click **Install / Update**.
6. Wait for Carbon Tracker to download, build, initialize MySQL, and start.
7. Use **Open Carbon Tracker** to open the local application.

Docker Desktop installation does **not** automatically install Carbon Tracker. This separation is intentional.

### Existing installation

Use **Install / Update** to check for an upstream update. Use **Start** and **Stop** for normal operation.

## Build from source

Prerequisites:

- Go 1.23+
- Python 3.10+ (standard library only; used by the PE icon embedding tool)

From PowerShell:

```powershell
./build.ps1
```

The output is created in `dist/`.

## Embedded Windows icon

`assets/Carbon-Tracker.ico` is embedded into the executable as Windows `RT_ICON` / `RT_GROUP_ICON` resources by `tools/embed_icon.py`. The application also loads resource ID `1` for its title-bar/taskbar icon, so an external `.ico` file is not required at runtime.

## Code signing

The repository includes guidance for Microsoft Artifact Signing and open-source signing through SignPath Foundation. See [`docs/CODE_SIGNING.md`](docs/CODE_SIGNING.md).

> A valid Authenticode signature is strongly recommended for public Windows releases, but signing alone does not guarantee that Microsoft Defender SmartScreen will never show a reputation warning for a new binary.

## License status — action required before public open-source release

As of 8 August 2026, the upstream Sustainability Monitoring Hub repository does not contain a `LICENSE` file or an explicit license declaration. A public GitHub repository is not automatically open source merely because its source is visible.

Before publishing this launcher as open source, choose an OSI-approved license for the Sustainability Monitoring Hub and apply the **same license** to this launcher. See [`LICENSE_REQUIRED.md`](LICENSE_REQUIRED.md).

## Security

Do not commit signing certificates, private keys, `.env` files, generated database passwords, Azure client secrets, or other credentials. See [`SECURITY.md`](SECURITY.md).

## Signed releases

Production Windows binaries are designed to be built and signed by GitHub Actions using Microsoft Azure Artifact Signing with GitHub OIDC. The private signing key never resides in this repository or in GitHub secrets. See [`docs/AZURE_ARTIFACT_SIGNING_SETUP.md`](docs/AZURE_ARTIFACT_SIGNING_SETUP.md).

Release tags such as `v4.4.1` trigger `.github/workflows/release.yml`, which builds the launcher, embeds the leaf icon, signs and timestamps the executable, verifies Authenticode, creates `SHA256SUMS.txt`, and publishes the GitHub Release.
