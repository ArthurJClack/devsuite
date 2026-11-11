# hq installer for windows. run in powershell:
#   irm https://raw.githubusercontent.com/ArthurJClack/devsuite/main/install.ps1 | iex
# puts hq.exe in %LOCALAPPDATA%\Programs\hq, adds start menu + desktop shortcuts and opens it.
# hq keeps itself updated after that.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$dir = Join-Path $env:LOCALAPPDATA 'Programs\hq'
$exe = Join-Path $dir 'hq.exe'
New-Item -ItemType Directory -Force -Path $dir | Out-Null

Write-Host "getting hq (fish market studios)..." -ForegroundColor Cyan
Get-Process hq -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $exe } | Stop-Process -Force
Invoke-WebRequest 'https://github.com/ArthurJClack/devsuite/releases/latest/download/hq.exe' -OutFile $exe -UseBasicParsing
Unblock-File $exe

$w = New-Object -ComObject WScript.Shell
$links = @(
  (Join-Path ([Environment]::GetFolderPath('Programs')) 'hq - fish market studios.lnk'),
  (Join-Path ([Environment]::GetFolderPath('Desktop')) 'hq.lnk')
)
foreach ($l in $links) {
  $s = $w.CreateShortcut($l); $s.TargetPath = $exe; $s.WorkingDirectory = $dir
  $s.Description = 'fish market studios hq'; $s.Save()
}

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
  Write-Host "git isn't installed - hq needs it." -ForegroundColor Yellow
  if (Get-Command winget -ErrorAction SilentlyContinue) {
    Write-Host "installing git with winget..."
    winget install --id Git.Git -e --source winget --accept-package-agreements --accept-source-agreements | Out-Null
  } else { Write-Host "grab it from https://git-scm.com/downloads" }
}

Start-Process $exe -WorkingDirectory $dir
Write-Host "done. hq is opening in your browser (start menu: 'hq - fish market studios')." -ForegroundColor Green
