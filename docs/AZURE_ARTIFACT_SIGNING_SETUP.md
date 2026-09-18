# Azure Artifact Signing + GitHub Actions setup

This repository is designed to sign Windows release executables with **Azure Artifact Signing** and **GitHub OpenID Connect (OIDC)**. No Azure client secret, PFX file, USB token, or private signing key is stored in GitHub.

## Architecture

1. GitHub Actions builds the Windows launcher on `windows-2025`.
2. `build.ps1` embeds `assets/Carbon-Tracker.ico` into the EXE before signing.
3. GitHub requests a short-lived OIDC token for the `release-signing` environment.
4. Azure trusts that GitHub identity through a federated credential.
5. `azure/artifact-signing-action@v2` signs the EXE with the configured Public Trust certificate profile.
6. The workflow adds an RFC3161 timestamp, verifies the Authenticode signature, calculates SHA-256, and publishes the GitHub Release.

The signing step must always run **after** icon/resource embedding. Any modification to the EXE after signing invalidates the signature.

## 1. Azure prerequisites

Use a paid Azure subscription supported by Artifact Signing.

In the Azure portal:

1. Register/use the `Microsoft.CodeSigning` resource provider if required.
2. Create an **Artifact Signing account**.
3. Create and complete **identity validation** for the publisher/person or organization that will appear in the public signing identity.
4. Create a **Public Trust** certificate profile associated with that validated identity.
5. Record:
   - Azure subscription ID
   - Microsoft Entra tenant ID
   - Artifact Signing account name
   - Certificate profile name
   - Artifact Signing endpoint shown for the account/region, e.g. `https://eus.codesigning.azure.net/`

Identity validation itself is completed in the Azure portal.

## 2. Create a Microsoft Entra app for GitHub OIDC

The following uses an app registration/service principal. Replace the placeholders before running.

```powershell
$GitHubOwner = '<GITHUB-OWNER>'
$GitHubRepo = 'Carbon-Tracker-Windows-Launcher'
$AppName = 'github-carbon-tracker-artifact-signing'

$appId = az ad app create --display-name $AppName --query appId -o tsv
az ad sp create --id $appId | Out-Null
$spObjectId = az ad sp show --id $appId --query id -o tsv

Write-Host "AZURE_CLIENT_ID=$appId"
Write-Host "Service principal object ID=$spObjectId"
```

## 3. Create the GitHub federated credential

This project deliberately binds Azure trust to the GitHub **environment** named `release-signing`, rather than allowing every branch to request a signing token.

Create `federated-credential.json` locally:

```json
{
  "name": "github-carbon-tracker-release-signing",
  "issuer": "https://token.actions.githubusercontent.com",
  "subject": "repo:<GITHUB-OWNER>/Carbon-Tracker-Windows-Launcher:environment:release-signing",
  "description": "GitHub Actions OIDC for signed Carbon Tracker releases",
  "audiences": ["api://AzureADTokenExchange"]
}
```

Then run:

```powershell
az ad app federated-credential create `
  --id $appId `
  --parameters .\federated-credential.json
```

The owner/repository spelling and case should match GitHub exactly.

## 4. Grant only the signing role

Assign the service principal the **Artifact Signing Certificate Profile Signer** role at the certificate-profile scope.

```powershell
$SubscriptionId = '<AZURE-SUBSCRIPTION-ID>'
$ResourceGroup = '<RESOURCE-GROUP>'
$SigningAccount = '<ARTIFACT-SIGNING-ACCOUNT>'
$CertificateProfile = '<CERTIFICATE-PROFILE>'

$scope = "/subscriptions/$SubscriptionId/resourceGroups/$ResourceGroup/providers/Microsoft.CodeSigning/codeSigningAccounts/$SigningAccount/certificateProfiles/$CertificateProfile"

az role assignment create `
  --assignee-object-id $spObjectId `
  --assignee-principal-type ServicePrincipal `
  --role 'Artifact Signing Certificate Profile Signer' `
  --scope $scope
```

Do not grant Owner or Contributor merely to make signing work. The certificate-profile signer role is sufficient for signing.

## 5. Configure GitHub

In the GitHub repository go to **Settings -> Environments -> New environment** and create:

`release-signing`

For a stronger release gate, optionally configure environment protection such as required reviewers and restrict deployments to release tags.

In **Settings -> Secrets and variables -> Actions**, configure these repository or `release-signing` environment secrets:

- `AZURE_CLIENT_ID` - the app registration application/client ID
- `AZURE_TENANT_ID` - Microsoft Entra tenant ID
- `AZURE_SUBSCRIPTION_ID` - Azure subscription ID

No `AZURE_CLIENT_SECRET` is used.

Configure these GitHub Actions **variables**:

- `ARTIFACT_SIGNING_ENDPOINT` - region endpoint including trailing `/`
- `ARTIFACT_SIGNING_ACCOUNT` - Artifact Signing account name
- `ARTIFACT_SIGNING_PROFILE` - Public Trust certificate profile name

## 6. Release a signed version

Commit `.github/workflows/release.yml`, then create and push a semantic version tag:

```powershell
git tag v4.4.1
git push origin v4.4.1
```

The workflow will:

- build the EXE;
- embed the icon;
- authenticate to Azure using OIDC;
- sign and timestamp it;
- verify that Windows reports a valid Authenticode signature;
- generate `SHA256SUMS.txt`;
- upload the signed build as a workflow artifact;
- create the GitHub Release and attach the signed EXE and checksum.

## 7. First-run validation

Before announcing the first release, download the EXE from the GitHub Release onto a clean Windows 11 machine and check:

```powershell
Get-AuthenticodeSignature .\CarbonTrackerLauncher-v4.4.1.exe | Format-List *
```

You should see `Status : Valid` and the expected publisher identity.

Also inspect **Properties -> Digital Signatures** and run the full Docker Desktop + fresh Carbon Tracker installation sequence.

## 8. SmartScreen expectations

Artifact Signing gives the executable a publicly trusted, verifiable publisher signature. SmartScreen can still consider application/publisher reputation, especially for a brand-new publisher or new distribution history, so the correct goal is a consistently signed publisher identity rather than assuming one signature can guarantee zero warnings on every Windows system.

## 9. Renewal and maintenance

Azure Artifact Signing manages the certificate and private key lifecycle in the service. Keep the identity validation current and keep using the same signing identity/profile for releases. The RFC3161 timestamp is important because it allows a valid historical signature to remain verifiable after the short-lived signing certificate itself rotates/expires.