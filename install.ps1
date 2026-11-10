# hq installer for windows. run in powershell:
#   irm https://raw.githubusercontent.com/ArthurJClack/devsuite/main/install.ps1 | iex
# downloads the latest hq.exe and runs it. hq installs itself to %LOCALAPPDATA%\Programs\hq,
# adds start menu + desktop shortcuts, opens in your browser, and keeps itself updated.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

Write-Host "getting hq (fish market studios)..." -ForegroundColor Cyan
$tmp = Join-Path $env:TEMP 'hq-setup.exe'
Invoke-WebRequest 'https://github.com/ArthurJClack/devsuite/releases/latest/download/hq.exe' -OutFile $tmp -UseBasicParsing
Unblock-File $tmp

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
  Write-Host "git isn't installed - hq needs it." -ForegroundColor Yellow
  if (Get-Command winget -ErrorAction SilentlyContinue) {
    Write-Host "installing git with winget..."
    winget install --id Git.Git -e --source winget --accept-package-agreements --accept-source-agreements | Out-Null
  } else {
    Write-Host "grab it from https://git-scm.com/downloads"
  }
}

Start-Process $tmp
Write-Host "done. hq is opening in your browser (start menu: 'hq - fish market studios')." -ForegroundColor Green
