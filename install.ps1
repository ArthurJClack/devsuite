# hq installer for windows. run in powershell:
#   irm https://raw.githubusercontent.com/ArthurJClack/devsuite/main/install.ps1 | iex
# puts hq.exe in %LOCALAPPDATA%\Programs\hq, adds start menu + desktop shortcuts and opens it.
# hq keeps itself updated after that. no admin needed, unless defender blocks it (see below).
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$dir = Join-Path $env:LOCALAPPDATA 'Programs\hq'
$exe = Join-Path $dir 'hq.exe'
$url = 'https://github.com/ArthurJClack/devsuite/releases/latest/download/hq.exe'

function Say($msg, $color = 'Gray') { Write-Host $msg -ForegroundColor $color }

function Get-Hq {
  # true if hq.exe downloaded and windows lets us use it
  Get-Process hq -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $exe } | Stop-Process -Force -ErrorAction SilentlyContinue
  Start-Sleep -Milliseconds 300
  try {
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $tmp = "$exe.download"
    Invoke-WebRequest $url -OutFile $tmp -UseBasicParsing
    Move-Item -Force $tmp $exe
    Start-Sleep -Seconds 2   # let antivirus look at it before we touch it
    if (-not (Test-Path $exe)) { return $false }
    Unblock-File $exe
    ([IO.File]::OpenRead($exe)).Close()
    return $true
  } catch {
    Say "  ($($_.Exception.Message))" DarkGray
    return $false
  }
}

Say "getting hq (fish market studios)..." Cyan
$ok = Get-Hq

if (-not $ok) {
  Say ""
  Say "windows blocked hq.exe ('access is denied')." Yellow
  Say "that's windows defender: hq isn't code-signed yet, so defender's guesser flags it. it's a false positive."
  Say "fix: add an exception for just the hq folder ($dir). that needs admin, so windows will ask."
  $ans = Read-Host "add the exception and try again? (y/n)"
  if ($ans -match '^(y|yes)$') {
    try {
      Start-Process powershell -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList "-NoProfile -Command Add-MpPreference -ExclusionPath '$dir'"
      Say "exception added, downloading again..." Cyan
      $ok = Get-Hq
    } catch {
      Say "couldn't get admin ($($_.Exception.Message))." Yellow
    }
  }
  if (-not $ok) {
    Say ""
    Say "still blocked. do it by hand:" Yellow
    Say "  windows security > virus & threat protection > manage settings > exclusions > add an exclusion > folder"
    Say "  pick: $dir"
    Say "  (paste %LOCALAPPDATA%\Programs into the address bar, make an 'hq' folder if it's missing)"
    Say "  then run this installer again."
    Say "on a work/school pc its admin may have to allow it."
    return
  }
}

$w = New-Object -ComObject WScript.Shell
$links = @(
  (Join-Path ([Environment]::GetFolderPath('Programs')) 'hq - fish market studios.lnk'),
  (Join-Path ([Environment]::GetFolderPath('Desktop')) 'hq.lnk')
)
foreach ($l in $links) {
  try {
    $s = $w.CreateShortcut($l); $s.TargetPath = $exe; $s.WorkingDirectory = $dir
    $s.Description = 'fish market studios hq'; $s.Save()
  } catch { }
}

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
  Say "git isn't installed - hq needs it." Yellow
  if (Get-Command winget -ErrorAction SilentlyContinue) {
    Say "installing git with winget..."
    winget install --id Git.Git -e --source winget --accept-package-agreements --accept-source-agreements | Out-Null
  } else { Say "grab it from https://git-scm.com/downloads" }
}

try {
  Start-Process $exe -WorkingDirectory $dir
  Say "done. hq is opening in your browser (start menu: 'hq - fish market studios')." Green
} catch {
  Say "hq is installed but windows wouldn't start it: $($_.Exception.Message)" Yellow
  Say "if that says access is denied, add the defender exception above, then open hq from the start menu."
}
