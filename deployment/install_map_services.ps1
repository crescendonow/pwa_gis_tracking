[CmdletBinding(SupportsShouldProcess)]
param(
    [Parameter(Mandatory = $true)] [string] $NssmPath,
    [Parameter(Mandatory = $true)] [string] $MartinExe,
    [Parameter(Mandatory = $true)] [string] $MapSyncExe,
    [Parameter(Mandatory = $true)] [string] $EnvironmentFile,
    [string] $ProjectRoot = (Split-Path -Parent $PSScriptRoot),
    [switch] $StartServices
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Resolve-RequiredFile([string] $Path, [string] $Label) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "$Label not found: $Path"
    }
    return (Resolve-Path -LiteralPath $Path).Path
}

$nssm = Resolve-RequiredFile $NssmPath "NSSM"
$martin = Resolve-RequiredFile $MartinExe "Martin executable"
$mapSync = Resolve-RequiredFile $MapSyncExe "map sync executable"
$envFile = Resolve-RequiredFile $EnvironmentFile "environment file"
$root = (Resolve-Path -LiteralPath $ProjectRoot).Path
$martinConfig = Resolve-RequiredFile (Join-Path $root "martin\config.yaml") "Martin config"
$logDirectory = Join-Path $root "logs\map-services"
New-Item -ItemType Directory -Path $logDirectory -Force | Out-Null

$environment = @(
    Get-Content -LiteralPath $envFile |
        ForEach-Object { $_.Trim() } |
        Where-Object { $_ -and -not $_.StartsWith("#") }
)
foreach ($required in @("DATABASE_URL", "MONGO_URI")) {
    if (-not ($environment | Where-Object { $_ -match "^$required=.+" })) {
        throw "$required is required in $envFile"
    }
}

function Set-NssmService {
    param(
        [string] $Name,
        [string] $Application,
        [string] $Arguments,
        [string[]] $ServiceEnvironment
    )
    $existing = Get-Service -Name $Name -ErrorAction SilentlyContinue
    if (-not $existing) {
        & $nssm install $Name $Application | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "Could not install $Name" }
    }
    & $nssm set $Name Application $Application | Out-Null
    & $nssm set $Name AppDirectory $root | Out-Null
    & $nssm set $Name AppParameters $Arguments | Out-Null
    & $nssm set $Name Start SERVICE_AUTO_START | Out-Null
    & $nssm set $Name AppExit Default Restart | Out-Null
    & $nssm set $Name AppStdout (Join-Path $logDirectory "$Name.out.log") | Out-Null
    & $nssm set $Name AppStderr (Join-Path $logDirectory "$Name.err.log") | Out-Null
    & $nssm set $Name AppRotateFiles 1 | Out-Null
    & $nssm set $Name AppRotateBytes 10485760 | Out-Null
    & $nssm set $Name AppEnvironmentExtra $ServiceEnvironment | Out-Null
}

if ($PSCmdlet.ShouldProcess("pwa-gis-map-martin and pwa-gis-map-sync", "Install/update NSSM services")) {
    $databaseEnvironment = @($environment | Where-Object { $_.StartsWith("DATABASE_URL=") })
    Set-NssmService -Name "pwa-gis-map-martin" -Application $martin -Arguments "--config `"$martinConfig`"" -ServiceEnvironment $databaseEnvironment
    Set-NssmService -Name "pwa-gis-map-sync" -Application $mapSync -Arguments "" -ServiceEnvironment $environment
    if ($StartServices) {
        foreach ($name in @("pwa-gis-map-martin", "pwa-gis-map-sync")) {
            $service = Get-Service -Name $name
            if ($service.Status -eq "Running") {
                Restart-Service -Name $name
            } else {
                Start-Service -Name $name
            }
        }
    }
}
