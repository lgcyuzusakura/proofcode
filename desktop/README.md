# ProofCode Desktop Preview

Run from PowerShell at the repository root:

```powershell
.\desktop\start-desktop.ps1
```

The launcher builds `frontend/`, serves the static UI on `127.0.0.1:4173`, and opens it in an isolated Edge App window. The window uses the same React UI as the Web client. The full local Go/Wails adapter still requires the Go and Wails toolchain on the host.
