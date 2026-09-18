# Windows code signing

Production releases use **Azure Artifact Signing + GitHub Actions OIDC**.

See [AZURE_ARTIFACT_SIGNING_SETUP.md](AZURE_ARTIFACT_SIGNING_SETUP.md) for the complete one-time Azure and GitHub setup.

The production workflow is `.github/workflows/release.yml`.

Important rules:

- Embed the Windows icon/resources **before** Authenticode signing.
- Never store a PFX, private key, client secret, or signing token in this repository.
- GitHub OIDC requires `permissions: id-token: write`.
- The Azure identity needs `Artifact Signing Certificate Profile Signer` at the certificate-profile scope.
- Use SHA-256 and RFC3161 timestamping (`http://timestamp.acs.microsoft.com`).
- Verify the final EXE after signing.

Artifact Signing substantially improves publisher trust and provides a publicly verifiable signature, but SmartScreen also uses reputation signals; a valid signature is not an absolute promise that a brand-new publisher will never see a warning.
