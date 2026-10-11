param([string]$Version = "1.2.0")
$ErrorActionPreference = "Stop"
if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw "Invalid version" }
$repoRoot = (Resolve-Path "$PSScriptRoot\..").Path
Push-Location $repoRoot
try {
    foreach ($arch in @("amd64", "arm64")) {
        New-Item -ItemType Directory -Force "dist\windows-$arch" | Out-Null
        $env:GOOS = "windows"
        $env:GOARCH = $arch
        $env:CGO_ENABLED = "0"
        go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$Version" -o "dist\windows-$arch\Zwoice.exe" ./cmd/zwoice
        if ($LASTEXITCODE -ne 0) { throw "Go build failed" }
    }
    $goRoot = go env GOROOT
    Copy-Item "$goRoot\LICENSE" "dist\GO-LICENSE.txt"
    & "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe" "/DAppVersion=$Version" "/DSourceRoot=$repoRoot" "packaging\windows\zwoice.iss"
    if ($LASTEXITCODE -ne 0) { throw "Installer build failed" }
} finally {
    Remove-Item Env:\GOOS, Env:\GOARCH, Env:\CGO_ENABLED -ErrorAction SilentlyContinue
    Pop-Location
}
