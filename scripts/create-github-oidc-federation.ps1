param(
    [Parameter(Mandatory=$true)][string]$GitHubOwner,
    [Parameter(Mandatory=$true)][string]$GitHubRepo,
    [Parameter(Mandatory=$true)][string]$SubscriptionId,
    [Parameter(Mandatory=$true)][string]$ResourceGroup,
    [Parameter(Mandatory=$true)][string]$SigningAccount,
    [Parameter(Mandatory=$true)][string]$CertificateProfile,
    [string]$AppName = 'github-carbon-tracker-artifact-signing',
    [string]$GitHubEnvironment = 'release-signing'
)

$ErrorActionPreference = 'Stop'

if (-not (Get-Command az -ErrorAction SilentlyContinue)) {
    throw 'Azure CLI (az) is required.'
}

$appId = az ad app create --display-name $AppName --query appId -o tsv
if ($LASTEXITCODE -ne 0 -or -not $appId) { throw 'Failed to create Entra app registration.' }

az ad sp create --id $appId | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to create service principal.' }

$spObjectId = az ad sp show --id $appId --query id -o tsv
if ($LASTEXITCODE -ne 0 -or -not $spObjectId) { throw 'Failed to resolve service principal object ID.' }

$subject = "repo:$GitHubOwner/$GitHubRepo`:environment:$GitHubEnvironment"
$tmp = Join-Path $env:TEMP 'carbon-tracker-github-federated-credential.json'
@{
    name = 'github-carbon-tracker-release-signing'
    issuer = 'https://token.actions.githubusercontent.com'
    subject = $subject
    description = 'GitHub Actions OIDC for signed Carbon Tracker releases'
    audiences = @('api://AzureADTokenExchange')
} | ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8 $tmp

az ad app federated-credential create --id $appId --parameters $tmp | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to create GitHub OIDC federated credential.' }

$scope = "/subscriptions/$SubscriptionId/resourceGroups/$ResourceGroup/providers/Microsoft.CodeSigning/codeSigningAccounts/$SigningAccount/certificateProfiles/$CertificateProfile"
az role assignment create `
  --assignee-object-id $spObjectId `
  --assignee-principal-type ServicePrincipal `
  --role 'Artifact Signing Certificate Profile Signer' `
  --scope $scope | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to assign Artifact Signing Certificate Profile Signer role.' }

Remove-Item $tmp -Force -ErrorAction SilentlyContinue

$tenantId = az account show --query tenantId -o tsv
Write-Host ''
Write-Host 'GitHub OIDC federation configured.' -ForegroundColor Green
Write-Host "AZURE_CLIENT_ID=$appId"
Write-Host "AZURE_TENANT_ID=$tenantId"
Write-Host "AZURE_SUBSCRIPTION_ID=$SubscriptionId"
Write-Host "GitHub OIDC subject=$subject"
Write-Host ''
Write-Host 'Configure ARTIFACT_SIGNING_ENDPOINT, ARTIFACT_SIGNING_ACCOUNT, and ARTIFACT_SIGNING_PROFILE as GitHub Actions variables.'
