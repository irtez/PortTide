$ErrorActionPreference = 'Stop'
# Official compiler, portable mode: no registry installation or associations.
$projectRoot = Split-Path -Parent $PSScriptRoot
$compilerRoot = Join-Path $projectRoot '.tools\inno'
$setupPath = Join-Path $projectRoot '.tools\innosetup-6.7.4.exe'
if (Test-Path -LiteralPath (Join-Path $compilerRoot 'ISCC.exe')) { Write-Output (Join-Path $compilerRoot 'ISCC.exe'); exit 0 }
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $setupPath) | Out-Null
Invoke-WebRequest -Uri 'https://github.com/jrsoftware/issrc/releases/download/is-6_7_4/innosetup-6.7.4.exe' -OutFile $setupPath
if ((Get-FileHash -LiteralPath $setupPath -Algorithm SHA256).Hash -ne 'A549929319F9120716677C685535CA4671E4B937B5FDC87E2585F32D8EB30E8C') { throw 'Inno Setup checksum mismatch' }
$args = @('/PORTABLE=1', '/CURRENTUSER', '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/NOICONS', ('/DIR="' + $compilerRoot + '"'))
$process = Start-Process -FilePath $setupPath -ArgumentList $args -WindowStyle Hidden -Wait -PassThru
if ($process.ExitCode -ne 0) { throw 'Portable compiler extraction failed' }
Write-Output (Join-Path $compilerRoot 'ISCC.exe')
