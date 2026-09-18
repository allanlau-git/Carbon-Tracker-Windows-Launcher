# Azure Artifact Signing + GitHub Actions setup

This repository signs Windows release executables with Azure Artifact Signing and GitHub OpenID Connect (OIDC). No Azure client secret, PFX file, USB token, or private signing key is stored in GitHub.

## Architecture

1. GitHub Actions builds the Windows launcher on Windows.
2. build.ps1 embeds assets/Carbon-Tracker.ico before signing.
3. GitHub requests a short-lived OIDC token for the release-signing environment.
4. Azure trusts that GitHub identity through a federated credential.
5. azure/artifact-signing-action@v2 signs the EXE with the configured Public Trust certificate profile.
6. The workflow timestamps, verifies Authenticode, calculates SHA-256, and publishes the GitHub Release.

Any modification to the EXE after signing invalidates the signature.

## Azure prerequisites

In the Azure portal:

1. Create an Artifact Signing account.
2. Complete identity validation for the publisher/person or organization.
3. Create a Public Trust certificate profile.
4. Record the Azure subscription ID, tenant ID, signing account name, certificate profile name, and regional Artifact Signing endpoint.

## Configure GitHub OIDC

Use the supplied scripts/create-github-oidc-federation.ps1 helper after running az login.

Repository owner: allanlau-git
Repository: Carbon-Tracker-Windows-Launcher
GitHub environment: release-signing

Expected OIDC subject:

repo:allanlau-git/Carbon-Tracker-Windows-Launcher:environment:release-signing

Grant the service principal only the Artifact Signing Certificate Profile Signer role at the certificate-profile scope.

## GitHub configuration

Create the environment release-signing under Settings -> Environments.

Add these GitHub Actions secrets:

- AZURE_CLIENT_ID
- AZURE_TENANT_ID
- AZURE_SUBSCRIPTION_ID

No AZURE_CLIENT_SECRET is used.

Add these GitHub Actions variables:

- ARTIFACT_SIGNING_ENDPOINT
- ARTIFACT_SIGNING_ACCOUNT
- ARTIFACT_SIGNING_PROFILE

## Release

Push a semantic version tag such as v4.4.1. The release workflow builds the executable, embeds the icon, signs and timestamps it, verifies the signature, creates SHA256SUMS.txt, and publishes the GitHub Release.

Before announcing the first release, download the EXE to a clean Windows machine and verify it with Get-AuthenticodeSignature and Properties -> Digital Signatures.

## SmartScreen

Artifact Signing provides a publicly trusted publisher signature. SmartScreen can still consider application and publisher reputation, especially for a new publisher. Use the same signing identity consistently for releases.

## Maintenance

Keep Azure identity validation current. RFC3161 timestamping preserves historical signature validity after signing certificate rotation or expiry.
