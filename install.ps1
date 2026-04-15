$ErrorActionPreference = "Stop"

$Repo = "AgusRdz/probe"
$InstallDir = if ($env:PROBE_INSTALL_DIR) { $env:PROBE_INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\probe" }

# Detect architecture
$Arch = if ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture -eq [System.Runtime.InteropServices.Architecture]::Arm64) {
    "arm64"
} else {
    "amd64"
}

$Binary = "probe-windows-$Arch.exe"

# Get latest version
if (-not $env:PROBE_VERSION) {
    $Release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
    $env:PROBE_VERSION = $Release.tag_name
}

if (-not $env:PROBE_VERSION) {
    Write-Error "failed to determine latest version"
    exit 1
}

$Url = "https://github.com/$Repo/releases/download/$($env:PROBE_VERSION)/$Binary"

Write-Host "installing probe $($env:PROBE_VERSION) (windows/$Arch)..."

# Create install dir
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

# Download binary
$Destination = Join-Path $InstallDir "probe.exe"
Invoke-WebRequest -Uri $Url -OutFile $Destination

Write-Host "installed probe to $Destination"
Write-Host ""

# Add to user PATH if not already present
$UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
$CleanInstallDir = $InstallDir.TrimEnd("\")
$PathParts = $UserPath -split ";" | ForEach-Object { $_.TrimEnd("\") }

if ($PathParts -notcontains $CleanInstallDir) {
    $NewUserPath = "$InstallDir;$UserPath"
    [Environment]::SetEnvironmentVariable("PATH", $NewUserPath, "User")
    Write-Host "added $InstallDir to PATH"
}

# Update current session PATH so probe can be used immediately
$CurrentPathParts = $env:PATH -split ";" | ForEach-Object { $_.TrimEnd("\") }
if ($CurrentPathParts -notcontains $CleanInstallDir) {
    $env:PATH = "$InstallDir;$env:PATH"
}

# Notify system of PATH change
$HWND_BROADCAST = [IntPtr]0xffff
$WM_SETTINGCHANGE = 0x001a
$MethodDefinition = @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Auto)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, IntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out IntPtr lpdwResult);
'@
$User32 = Add-Type -MemberDefinition $MethodDefinition -Name "User32" -Namespace "Win32" -PassThru
$result = [IntPtr]::Zero
$User32::SendMessageTimeout($HWND_BROADCAST, $WM_SETTINGCHANGE, [IntPtr]::Zero, "Environment", 2, 100, [ref]$result) | Out-Null

Write-Host "next steps:"
Write-Host ""
Write-Host "  probe intercept --target http://localhost:3000"
Write-Host "  probe list"
Write-Host "  probe export --format openapi > api.yaml"
