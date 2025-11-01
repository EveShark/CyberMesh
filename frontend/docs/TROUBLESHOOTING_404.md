# 🔧 Troubleshooting 404 Error

## Issue: Getting 404 on http://localhost:3000

You're right - the 404 is likely due to **missing backend connection or startup issues**. Here's how to fix it:

---

## ✅ Quick Fixes (Try These First)

### 1. **Clean Start** (Most Common Fix)
```bash
cd B:\CyberMesh\frontend-new

# Stop any running dev server (Ctrl+C)

# Clear build cache
Remove-Item -Recurse -Force .next

# Restart dev server
npm run dev
```

**Wait 30-60 seconds for compilation**, then visit:
- http://localhost:3000

---

### 2. **Check Port Conflicts**
```bash
# Check if port 3000 is already in use
netstat -ano | findstr :3000

# If in use, kill the process or use different port
$env:PORT=3001; npm run dev
```

---

### 3. **Verify Build Artifacts**
```bash
# Check if .next directory exists
Test-Path .next

# If missing, rebuild
npm run build
npm run dev
```

---

## 🔍 Root Cause Analysis

### The 404 is NOT because of:
❌ Backend not running - Frontend works standalone with mock APIs
❌ Missing environment variables - Frontend has defaults
❌ Database connection - Frontend doesn't need it directly

### The 404 IS likely because of:
✅ **Dev server still compiling** - Wait 30-60 seconds after `npm run dev`
✅ **Stale build cache** - Delete `.next` folder
✅ **Port already in use** - Another process using port 3000
✅ **Import errors at runtime** - Check terminal for errors

---

## 📊 Step-by-Step Diagnosis

### Step 1: Check Dev Server Status
```bash
cd B:\CyberMesh\frontend-new
npm run dev
```

**Look for these messages:**
```
✓ Compiled successfully
✓ Ready in 2.3s
○ Local:    http://localhost:3000
```

**If you see errors instead:**
- Read the error message carefully
- Most likely: Import path errors or missing dependencies

---

### Step 2: Check Browser Console

Open http://localhost:3000 and press F12 (Developer Tools)

**Check Console tab for errors:**
- Red errors = JavaScript issues
- 404 errors = Missing files or routes
- CORS errors = Backend connection issues (but won't cause 404 on homepage)

**Check Network tab:**
- Look for failed requests
- Check which file is giving 404

---

### Step 3: Verify Route Files Exist

```bash
# Check main page exists
Test-Path src\app\page.tsx
Test-Path app\overview.tsx

# Check other pages
Get-ChildItem src\app -Directory
```

**Expected output:**
```
✓ src\app\page.tsx exists
✓ app\overview.tsx exists

Directories:
ai-engine, api, blocks, consensus, investors, ledger, 
metrics, network, system-health, threats
```

---

### Step 4: Test Direct Routes

Try accessing different pages:
- http://localhost:3000/ (Homepage - Overview)
- http://localhost:3000/threats
- http://localhost:3000/consensus
- http://localhost:3000/blocks

**If some work but not others:**
- Specific page has import errors
- Check terminal for compilation errors for that route

---

## 🐛 Common Issues & Solutions

### Issue 1: "Cannot find module" errors in terminal

**Symptom:**
```
Error: Cannot find module '@/components/...'
```

**Solution:**
```bash
# Check if components.json is correct
cat components.json

# Verify tsconfig.json has correct paths
cat tsconfig.json | Select-String "paths"
```

**Should see:**
```json
"paths": {
  "@/*": ["./src/*"]
}
```

---

### Issue 2: Empty page / White screen

**Symptom:**
- Page loads but shows nothing
- No 404, just blank

**Solution:**
1. Check browser console for JavaScript errors
2. Check if components are rendering:
```bash
# Add console.log to overview.tsx
# In app/overview.tsx, add at top of component:
console.log("OverviewPage rendering")
```

---

### Issue 3: Hydration errors

**Symptom:**
```
Hydration failed because the initial UI does not match...
```

**Solution:**
```bash
# Some components need client-side only rendering
# Check if all "use client" directives are present
```

---

### Issue 4: Module not found at runtime

**Symptom:**
- Build succeeds
- Dev server starts
- But page shows 404 or error

**Solution:**
```bash
# Rebuild from scratch
Remove-Item -Recurse -Force .next, node_modules
npm install
npm run build
npm run dev
```

---

## 🔌 Backend Connection (Not Required for 404 Fix)

The frontend **works without backend** using mock APIs!

### Mock API Routes (Already Included)
Located in `src/app/api/`:
- `/api/nodes/health` - Mock node health data
- `/api/consensus/status` - Mock consensus status
- `/api/blocks` - Mock block data

### When You're Ready to Connect Backend

Update `.env.local`:
```env
NEXT_PUBLIC_API_BASE=http://localhost:8443/api
BACKEND_API_BASE=http://localhost:8443/api
```

Then start backend:
```bash
cd B:\CyberMesh\backend
go run cmd/cybermesh/main.go
```

**But this is NOT required to fix the 404!**

---

## 🎯 Most Likely Solution

Based on the error, here's what to do:

### Solution: Clean Restart
```powershell
# Navigate to frontend
cd B:\CyberMesh\frontend-new

# Stop any running server (Ctrl+C if running)

# Clean everything
Remove-Item -Recurse -Force .next -ErrorAction SilentlyContinue

# Start fresh
npm run dev
```

**Then wait 30-60 seconds** for:
```
✓ Compiled successfully
✓ Ready in X.Xs
○ Local: http://localhost:3000
```

**Then open browser:** http://localhost:3000

---

## 📸 What You Should See

### On Terminal (after npm run dev):
```
  ▲ Next.js 14.2.25
  - Local:        http://localhost:3000
  - Environments: .env.local

✓ Ready in 2.3s
○ Compiling / ...
✓ Compiled / in 543ms
```

### On Browser (http://localhost:3000):
- **Dark themed dashboard**
- **"System Status" header**
- **Cluster Overview section** with node tiles
- **Service Health grid**
- **Recent Threats table**
- **Traffic sparkline chart**
- **Sidebar on left** with navigation

---

## 🆘 Still Getting 404?

### Check These:

1. **Terminal shows compilation errors?**
   - Copy error message
   - Most likely: Import path issue
   - Check which file is mentioned in error

2. **Terminal shows "✓ Compiled successfully"?**
   - Try hard refresh: Ctrl+Shift+R
   - Try incognito mode
   - Try different browser

3. **Browser shows error in console?**
   - Open DevTools (F12)
   - Copy error from Console tab
   - Check Network tab for which request is 404

4. **Port 3000 in use?**
   ```bash
   # Use different port
   $env:PORT=3001; npm run dev
   # Then visit http://localhost:3001
   ```

---

## 💡 Quick Test

Run this to verify everything:

```powershell
cd B:\CyberMesh\frontend-new

# Check files exist
Write-Host "Checking files..."
@(
  "src\app\page.tsx",
  "app\overview.tsx",
  "src\components\node-status-grid.tsx",
  "src\lib\mode-context.tsx"
) | ForEach-Object {
  if (Test-Path $_) { Write-Host "✓ $_" } else { Write-Host "✗ $_ MISSING" }
}

# Check dependencies
Write-Host "`nChecking dependencies..."
if (Test-Path "node_modules") {
  Write-Host "✓ node_modules exists"
} else {
  Write-Host "✗ node_modules missing - run 'npm install'"
}

# Check build
Write-Host "`nChecking build..."
if (Test-Path ".next") {
  Write-Host "✓ .next exists (build cache present)"
} else {
  Write-Host "✗ .next missing - will be created on first run"
}

Write-Host "`nReady to start: npm run dev"
```

---

## ✅ Expected Behavior

When working correctly:

1. **Terminal:** Shows "✓ Compiled successfully"
2. **Browser:** Loads dark dashboard with data
3. **No errors** in browser console
4. **Sidebar works** (click different pages)
5. **Mock data shows** in tables and charts

**Backend NOT required** for basic functionality!

---

## 📞 Need Help?

If still seeing 404:
1. Copy exact error from terminal
2. Copy error from browser console (F12)
3. Take screenshot of what you see
4. Share which URL gives 404

---

**Most Common Fix:** Delete `.next` folder and restart dev server! 🚀
