param(
    [string]$Version = "dev",
    [string]$DistDir = ""
)

$ErrorActionPreference = "Stop"

$RootDir = Resolve-Path (Join-Path $PSScriptRoot "..")
if ($DistDir -eq "") {
    $DistDir = Join-Path $RootDir "dist"
}

New-Item -ItemType Directory -Force -Path $DistDir | Out-Null

function Build-Binary {
    param(
        [string]$Name,
        [string]$Package,
        [string]$TargetOS,
        [string]$TargetArch
    )

    $extension = ""
    if ($TargetOS -eq "windows") {
        $extension = ".exe"
    }

    $output = Join-Path $DistDir "$Name-$TargetOS-$TargetArch$extension"

    Write-Host "building $output"

    $env:CGO_ENABLED = "0"
    $env:GOOS = $TargetOS
    $env:GOARCH = $TargetArch

    go build `
        -trimpath `
        -ldflags "-s -w -X main.version=$Version" `
        -o $output `
        $Package
}

Build-Binary -Name "wasmcat-master" -Package "./cmd/master" -TargetOS "linux" -TargetArch "amd64"
Build-Binary -Name "wasmcat-worker" -Package "./cmd/worker" -TargetOS "linux" -TargetArch "amd64"
Build-Binary -Name "wasmcat-master" -Package "./cmd/master" -TargetOS "windows" -TargetArch "amd64"
Build-Binary -Name "wasmcat-worker" -Package "./cmd/worker" -TargetOS "windows" -TargetArch "amd64"

Copy-Item -Path (Join-Path $RootDir "packaging/systemd/wasmcat-*") -Destination $DistDir -Force

$checksumFile = Join-Path $DistDir "checksums.txt"
if (Test-Path $checksumFile) {
    Remove-Item -LiteralPath $checksumFile -Force
}

Get-ChildItem -LiteralPath $DistDir -File | Sort-Object Name | ForEach-Object {
    $hash = Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName
    "$($hash.Hash.ToLowerInvariant())  $($_.Name)" | Add-Content -LiteralPath $checksumFile
}

Write-Host "release artifacts written to $DistDir"
