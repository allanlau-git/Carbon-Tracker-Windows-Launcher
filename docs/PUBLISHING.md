# GitHub publishing guide

## Recommended repository metadata

**Repository name**

`Carbon-Tracker-Windows-Launcher`

**Description**

> Native Windows GUI installer and launcher for Sustainability Monitoring Hub / Carbon Tracker. Installs or updates Docker Desktop, deploys the local Docker stack, and manages start, stop, updates and logs.

**Website**

Use the upstream Sustainability Monitoring Hub repository or its public demo URL.

**Suggested topics**

`sustainability`, `carbon-accounting`, `ghg`, `greenhouse-gas`, `esg`, `windows`, `docker`, `streamlit`, `mysql`, `installer`, `launcher`, `carbon-tracker`

## Files to publish

Keep these in source control:

- `README.md`
- `LICENSE` (after the license decision is made)
- `CONTRIBUTING.md`
- `SECURITY.md`
- `CHANGELOG.md`
- `go.mod`
- `cmd/`
- `assets/`
- `tools/`
- `.github/workflows/build.yml`
- issue / pull request templates
- `docs/`

Publish compiled `.exe` files as **GitHub Release assets**, not as ordinary source files on the main branch.

## GitHub Release recommendation

Use semantic tags, for example:

`v4.4.0`

Attach:

- `CarbonTrackerLauncher-v4.4.0.exe` (signed production binary)
- `SHA256SUMS.txt`

Release notes should mention:

- supported Windows versions;
- that Docker Desktop can be installed separately;
- fresh install/update behavior;
- local URL (`http://localhost:8501`);
- data preservation behavior;
- known prerequisites/restart requirements;
- security/signature verification instructions.

## Repository health files

GitHub recommends a README and supports license, contribution guidelines, code of conduct and security policy files to communicate expectations to users and contributors. Add a code of conduct when you expect community contributions.

## Branch protection

Recommended for `main`:

- require pull requests before merge;
- require the Windows build check to pass;
- block force pushes;
- require review for workflow changes;
- enable Dependabot/security alerts where available.

## Release integrity

For each production tag:

1. Build from the tagged source.
2. Embed the icon before signing.
3. Authenticode-sign the final EXE.
4. Timestamp the signature.
5. Verify the signature.
6. Generate SHA-256 checksums from the signed binary.
7. Upload the signed binary and checksum as Release assets.

Never modify the EXE after signing; embedding an icon or changing resources after signing invalidates the signature.
