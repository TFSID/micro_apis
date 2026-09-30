$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$previous = @{
    GOOS = $env:GOOS
    GOARCH = $env:GOARCH
    CGO_ENABLED = $env:CGO_ENABLED
}

try {
    Set-Location $repoRoot
    $env:GOOS = 'linux'
    $env:GOARCH = 'arm64'
    $env:CGO_ENABLED = '0'
    & go build -trimpath -ldflags='-s -w' -o bootstrap ./cmd/api
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed with exit code $LASTEXITCODE"
    }

    Add-Type -AssemblyName System.IO.Compression
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $artifactDirectory = Join-Path $repoRoot '.build'
    New-Item -ItemType Directory -Force -Path $artifactDirectory | Out-Null
    $artifactPath = Join-Path $artifactDirectory 'micro-api.zip'
    if (Test-Path $artifactPath) {
        Remove-Item $artifactPath -Force
    }

    $archive = [System.IO.Compression.ZipFile]::Open(
        $artifactPath,
        [System.IO.Compression.ZipArchiveMode]::Create
    )
    try {
        $entry = $archive.CreateEntry('bootstrap')
        $entry.ExternalAttributes = -2115174400 # Unix regular file, mode 0755
        $source = [System.IO.File]::OpenRead((Join-Path $repoRoot 'bootstrap'))
        $destination = $entry.Open()
        try {
            $source.CopyTo($destination)
        }
        finally {
            $destination.Dispose()
            $source.Dispose()
        }
    }
    finally {
        $archive.Dispose()
    }
    Write-Host "Built Linux ARM64 Lambda package at $artifactPath."
}
finally {
    foreach ($name in $previous.Keys) {
        if ($null -eq $previous[$name]) {
            Remove-Item "Env:$name" -ErrorAction SilentlyContinue
        }
        else {
            Set-Item "Env:$name" $previous[$name]
        }
    }
}
