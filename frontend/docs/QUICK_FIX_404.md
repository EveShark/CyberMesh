# 🚀 Quick Fix for 404 Error

## The Problem
You're seeing a 404 error when visiting http://localhost:3000

## The Solution (30 seconds)

### Option 1: Use the Script (Easiest)
```powershell
cd B:\CyberMesh\frontend-new
.\START_FRESH.ps1
```

### Option 2: Manual Steps
```powershell
cd B:\CyberMesh\frontend-new

# Stop server if running (Ctrl+C)

# Delete cache
Remove-Item -Recurse -Force .next

# Start fresh
npm run dev
```

**Wait 30-60 seconds** for compilation, then open:
- **http://localhost:3000**

---

## Why This Happens

The 404 is **NOT because**:
- ❌ Backend is not running (frontend works standalone!)
- ❌ Database not connected (not needed for frontend!)
- ❌ Missing environment variables (has defaults!)

The 404 **IS because**:
- ✅ **Stale build cache** in `.next` folder
- ✅ **Dev server still compiling** (wait 60 seconds!)
- ✅ **Port conflict** (another app using port 3000)

---

## What You Should See

### Terminal (after npm run dev):
```
  ▲ Next.js 14.2.25
  - Local: http://localhost:3000

✓ Ready in 2.3s
○ Compiling / ...
✓ Compiled / in 543ms
```

### Browser (http://localhost:3000):
✅ Dark themed dashboard
✅ "System Status" header
✅ Node status tiles (5 nodes)
✅ Service health grid
✅ Recent threats table
✅ Sidebar navigation

---

## Still Not Working?

### Try Different Port:
```powershell
$env:PORT=3001
npm run dev
# Then visit http://localhost:3001
```

### Check for Errors:
```powershell
# Look at terminal output
# Any red "Error:" messages?
# Copy and share them
```

### Hard Refresh Browser:
- Windows: `Ctrl + Shift + R`
- Or try Incognito mode

---

## Backend Connection (Later)

**You don't need the backend to fix the 404!**

The frontend has **mock APIs** built-in. When you're ready to connect the real backend:

1. Start backend:
   ```bash
   cd B:\CyberMesh\backend
   go run cmd/cybermesh/main.go
   ```

2. Backend runs on port **8443** (already configured in `.env.local`)

---

## Quick Test

```powershell
cd B:\CyberMesh\frontend-new

# Verify setup
Test-Path src\app\page.tsx  # Should return True
Test-Path app\overview.tsx  # Should return True
Test-Path node_modules      # Should return True

# Start
npm run dev
```

**Wait for "✓ Compiled successfully"**, then open browser!

---

## 🎯 Summary

**Most likely cause:** Stale build cache
**Fix:** Delete `.next` folder and restart
**Time:** 30 seconds
**Backend needed:** NO (works standalone with mock data)

Run `.\START_FRESH.ps1` and you're done! 🚀
