# Screenshot of the RO client window (physical pixels) -> PNG. Usage: shot.ps1 -Out path.png [-Scale 0.5]
param([string]$Out = "$env:TEMP\ro-shot.png", [double]$Scale = 0.5, [string]$Crop = "")
Add-Type -AssemblyName System.Drawing
Add-Type @"
using System; using System.Runtime.InteropServices;
public class RoWin {
  [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
}
"@
[RoWin]::SetProcessDPIAware() | Out-Null
$p = Get-Process RO-AILab -ErrorAction SilentlyContinue | ? { $_.MainWindowHandle -ne 0 } | Select -First 1
if (!$p) { Write-Output "client not running"; exit 1 }
$r = New-Object RoWin+RECT
[RoWin]::GetWindowRect($p.MainWindowHandle, [ref]$r) | Out-Null
$w = $r.R - $r.L; $h = $r.B - $r.T
if ($Crop) { $cx, $cy, $cw, $ch = $Crop.Split(",") | % { [int]$_ }; $r.L += $cx; $r.T += $cy; $w = $cw; $h = $ch }
$bmp = New-Object Drawing.Bitmap $w, $h
$g = [Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($r.L, $r.T, 0, 0, $bmp.Size)
$ow = [int]($w * $Scale); $oh = [int]($h * $Scale)
$small = New-Object Drawing.Bitmap $ow, $oh
$g2 = [Drawing.Graphics]::FromImage($small)
$g2.InterpolationMode = 'HighQualityBicubic'
$g2.DrawImage($bmp, 0, 0, $ow, $oh)
$small.Save($Out, [Drawing.Imaging.ImageFormat]::Png)
Write-Output "window $($r.L),$($r.T) ${w}x${h} -> $Out"
