param([switch]$Production)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$env:GOMODCACHE = Join-Path $root '.local/go-mod'
$env:GOCACHE = Join-Path $root '.local/go-build'
$env:TEST_ENV = '1'
if ($Production) {
    $runtime = Join-Path $root 'output/native-runtime/desktop-kernel'
    $app = Join-Path $runtime 'app'
    $env:GITOWL_KERNEL_ENTRY = Join-Path $app 'main.js'
    $env:GITOWL_KERNEL_ELECTRON = Join-Path $runtime 'electron.exe'
    $env:GITOWL_KERNEL_FIRST_RUN_RUNTIME = $runtime
} else {
    $app = Join-Path $root 'out'
    $env:GITOWL_KERNEL_ENTRY = Join-Path $app 'main.js'
    $env:GITOWL_KERNEL_ELECTRON = Join-Path $root 'node_modules/electron/dist/electron.exe'
    Remove-Item Env:GITOWL_KERNEL_FIRST_RUN_RUNTIME -ErrorAction SilentlyContinue
}
# Only the real original-kernel UI tests are opt-in here. Research Node-service
# tests are retained for comparison, never counted as native parity evidence.
Remove-Item Env:GITOWL_NATIVE_RUNTIME -ErrorAction SilentlyContinue
Remove-Item Env:GITOWL_NATIVE_SCREENSHOT_REPO -ErrorAction SilentlyContinue
foreach ($input in @($env:GITOWL_KERNEL_ENTRY, $env:GITOWL_KERNEL_ELECTRON, (Join-Path $app 'native-kernel.html'), (Join-Path $app 'git/cmd/git.exe'))) {
    if (-not (Test-Path $input -PathType Leaf)) { throw "Missing original kernel test input: $input" }
}
Push-Location $root
try {
    $modfile = (& python tools/prepare_mygo.py).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Verified MyGo preparation failed' }
    Push-Location app/native
    try {
        & go test "-modfile=$modfile" ./... -count=1
        if ($LASTEXITCODE -ne 0) { throw 'Original Desktop kernel / MyGo UI tests failed' }
        # A valid second launch must reach the original CLI/OAuth dispatcher
        # even when Windows delays the local socket's write-side EOF.
        & go test "-modfile=$modfile" github.com/egoist/mygo -run '^(TestGitowlSecondInstanceDoesNotWaitForSenderEOF|TestSingleInstance|TestSingleInstanceIdentifier)$' -count=1
        if ($LASTEXITCODE -ne 0) { throw 'Native single-instance launch framing tests failed' }
    } finally { Pop-Location }
} finally { Pop-Location }
