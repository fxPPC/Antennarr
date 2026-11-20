# Compile the Windows desktop installer from the matching native payload ZIP.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidatePattern('^v1\.1\.[0-9]+-beta\.[0-9]+$')][string]$Version,
    [Parameter(Mandatory)][ValidateSet('amd64', 'arm64')][string]$Arch,
    [Parameter(Mandatory)][string]$PayloadZip,
    [Parameter(Mandatory)][string]$OutputDir
)

$ErrorActionPreference = 'Stop'
$name = "antennarr-$Version-windows-$Arch"
$zip = Get-Item -LiteralPath $PayloadZip
if ($zip.Name -cne "$name.zip") {
    throw "Expected $name.zip, received $($zip.Name)"
}
$recipe = Join-Path $PSScriptRoot 'native/Antennarr.iss'
$work = Join-Path ([IO.Path]::GetTempPath()) ("antennarr-installer-" + [guid]::NewGuid())
$null = New-Item -ItemType Directory -Path $work
try {
    $payload = Join-Path $work 'payload'
    Expand-Archive -LiteralPath $zip.FullName -DestinationPath $payload
    $description = Get-Content -LiteralPath (Join-Path $payload 'README.txt') -TotalCount 1
    if ($description -cne "Antennarr $Version for desktop") {
        throw 'The native payload does not match the requested release.'
    }

    $setup = Join-Path $work 'innosetup-6.7.3.exe'
    $url = 'https://github.com/jrsoftware/issrc/releases/download/is-6_7_3/innosetup-6.7.3.exe'
    $sha256 = '9c73c3bae7ed48d44112a0f48e66742c00090bdb5bef71d9d3c056c66e97b732'
    Invoke-WebRequest -Uri $url -OutFile $setup
    if ((Get-FileHash -LiteralPath $setup -Algorithm SHA256).Hash -ne $sha256) {
        throw 'Unexpected SHA-256 for the Inno Setup compiler.'
    }

    $tools = Join-Path $work 'compiler'
    $argsForSetup = "/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /SP- /CURRENTUSER /PORTABLE=1 /DIR=`"$tools`""
    $install = Start-Process -FilePath $setup -ArgumentList $argsForSetup -Wait -PassThru
    if ($install.ExitCode -ne 0) {
        throw "Inno Setup installation failed with exit code $($install.ExitCode)"
    }

    $staging = New-Item -ItemType Directory -Path (Join-Path $work 'installer')
    & (Join-Path $tools 'ISCC.exe') "/DAppVersion=$Version" "/DAppArch=$Arch" "/DPayloadDir=$payload" "/DOutputDir=$($staging.FullName)" $recipe
    if ($LASTEXITCODE -ne 0) {
        throw "Inno Setup compilation failed with exit code $LASTEXITCODE"
    }
    $output = New-Item -ItemType Directory -Force -Path $OutputDir
    $installer = Copy-Item -LiteralPath (Join-Path $staging.FullName "$name-setup.exe") -Destination $output.FullName -Force -PassThru
    Get-FileHash -LiteralPath $installer.FullName -Algorithm SHA256
} finally {
    Remove-Item -LiteralPath $work -Recurse -Force
}
