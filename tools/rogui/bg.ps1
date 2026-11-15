# bg.ps1 - background helpers for the RO client: never moves the real mouse, never takes focus.
#   capture: PrintWindow(PW_RENDERFULLCONTENT) of the client area (works while the window is covered)
#   input:   PostMessage of mouse / keyboard messages straight to the game window
Add-Type -AssemblyName System.Drawing
if (-not ('RoBg' -as [type])) {
Add-Type @"
using System; using System.Runtime.InteropServices; using System.Drawing;
public class RoBg {
  [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();
  [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint flags);
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint msg, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern uint MapVirtualKey(uint code, uint type);
  [DllImport("user32.dll")] public static extern short VkKeyScan(char c);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
  [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
  public static Bitmap Capture(IntPtr h) {
    RECT wr; GetWindowRect(h, out wr); RECT cr; GetClientRect(h, out cr);
    POINT p = new POINT(); ClientToScreen(h, ref p);
    int ww = wr.R - wr.L, wh = wr.B - wr.T;
    Bitmap full = new Bitmap(ww, wh);
    using (Graphics g = Graphics.FromImage(full)) { IntPtr dc = g.GetHdc(); PrintWindow(h, dc, 2); g.ReleaseHdc(dc); }
    int ox = p.X - wr.L, oy = p.Y - wr.T;
    Bitmap client = full.Clone(new Rectangle(ox, oy, cr.R, cr.B), full.PixelFormat);
    full.Dispose();
    return client;
  }
  public static IntPtr XY(int x, int y) { return (IntPtr)((y << 16) | (x & 0xFFFF)); }
  public static IntPtr KeyL(uint vk, bool up, bool alt) {
    uint sc = MapVirtualKey(vk, 0);
    uint l = 1 | (sc << 16);
    if (alt) l |= (1u << 29);
    if (up) l |= (1u << 30) | (1u << 31);
    return (IntPtr)unchecked((int)l);
  }
}
"@ -ReferencedAssemblies System.Drawing
}
[RoBg]::SetProcessDPIAware() | Out-Null
