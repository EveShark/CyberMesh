# Start backend with output logging
$logFile = "B:\CyberMesh\backend_test.log"

Write-Host "Starting backend with logging to: $logFile"
Write-Host "Press Ctrl+C to stop"
Write-Host ""

cd "B:\CyberMesh\backend"
.\cybermesh.exe 2>&1 | Tee-Object -FilePath $logFile
