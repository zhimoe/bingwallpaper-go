$ErrorActionPreference = "Stop"

$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"

New-Item -ItemType Directory -Force -Path "dist" | Out-Null
go test ./...
go vet ./...
go build -trimpath -ldflags "-s -w" -o "dist/BingWallpaper-cli.exe" ./cmd/bingwallpaper
go build -trimpath -ldflags "-s -w -H windowsgui -X github.com/zhimoe/bingwallpaper/internal/app.fileLogging=true -X github.com/zhimoe/bingwallpaper/internal/app.guiBuild=true" -o "dist/BingWallpaper.exe" ./cmd/bingwallpaper

Write-Host "Built dist/BingWallpaper.exe and dist/BingWallpaper-cli.exe"
