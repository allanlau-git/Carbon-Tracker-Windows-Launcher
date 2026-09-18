# Security Policy

## Reporting a vulnerability

Please do not publish exploitable security issues in a public GitHub issue. Contact the repository maintainer privately and include reproduction steps, affected versions, and impact.

## Sensitive material that must never be committed

- Code-signing private keys or PFX/P12 files
- Azure client secrets
- Docker registry credentials
- Carbon Tracker `.env` files or generated passwords
- User database volumes, uploads, or exported logs containing confidential data

## Release integrity

Official release binaries should be built from a tagged commit, Authenticode-signed, timestamped, and published with a SHA-256 checksum.
