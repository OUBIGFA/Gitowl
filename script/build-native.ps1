param(
    [string]$BuildNumber = (Get-Date -Format 'yyyyMMddHHmmss'),
    [switch]$Preview,
    [string]$AcceptanceReceipt = ''
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
# Fixed packaging requires source-bound command and independent desktop review
# evidence. A missing receipt still exits before any build or installer action.
if (-not $Preview -and [string]::IsNullOrWhiteSpace($AcceptanceReceipt)) {
    throw 'Fixed installer delivery is blocked: complete Desktop kernel presentation parity requires a current native acceptance receipt (-AcceptanceReceipt).'
}
$runtimeManifest = Get-Content (Join-Path $root 'third_party/native-runtimes.json') -Raw -Encoding UTF8 | ConvertFrom-Json
if ($Preview -and ($runtimeManifest.schemaVersion -ne 2 -or $runtimeManifest.packaging.previewReady -ne $true)) {
    throw ('Preview packaging is blocked: original Desktop CLI/OAuth acceptance is pending. ' + ($runtimeManifest.packaging.blockers -join ' '))
}
if ($BuildNumber -notmatch '^[0-9]{14}$') { throw 'BuildNumber must use yyyyMMddHHmmss' }
$env:PATH = (Join-Path $root '.local/toolchains/node-v24.22.1-win-x64') + ';' + $env:PATH
$env:GOMODCACHE = Join-Path $root '.local/go-mod'
$env:GOCACHE = Join-Path $root '.local/go-build'
$env:GOTMPDIR = Join-Path $root '_temp/go-build'
New-Item -ItemType Directory -Force $env:GOTMPDIR | Out-Null

function Check-Exit([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "$Step failed (exit $LASTEXITCODE)" }
}

function Invoke-NativeAcceptance([string]$PackageDirectory = '') {
    $arguments = @('node_modules/ts-node/dist/bin.js', '-P', 'script/tsconfig.json', 'script/native-acceptance.ts', 'validate', '--receipt', $AcceptanceReceipt)
    if ($PackageDirectory) { $arguments += @('--package', $PackageDirectory) }
    $result = & node @arguments
    Check-Exit 'Source-bound native acceptance'
    return ($result | ConvertFrom-Json)
}

Push-Location $root
try {
    $acceptance = $null
    if (-not $Preview) {
        if ($env:MYGO_INSPECTOR -eq '1') { throw 'Inspector-enabled builds cannot be fixed installers' }
        $acceptance = Invoke-NativeAcceptance
    }
    $config = Get-Content 'app/native/mygo.json' -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($config.name -ne 'Gitowl' -or $config.version -ne '0.0.1' -or $config.identifier -ne 'io.github.oubigfa.gitowl') {
        throw 'The fixed test product identity changed; review installer/data compatibility first'
    }
    & git diff --quiet
    Check-Exit 'Stage the intended source snapshot before packaging'
    $untracked = & git ls-files --others --exclude-standard
    Check-Exit 'Read source file list'
    if ($untracked) { throw 'Stage or ignore the remaining source files before packaging' }
    $sourceTree = (& git write-tree).Trim()
    Check-Exit 'Read staged source tree'
    & python tools/check_repo.py
    Check-Exit 'Repository boundaries'
    & python -m unittest discover -s tests/repository -v
    Check-Exit 'Repository tests'
    & npm.cmd run check:features
    Check-Exit 'Upstream feature declarations'
    & npm.cmd run test:kernel
    Check-Exit 'Original Desktop Electron kernel and native presentation tests'
    & python tools/record_native_dependencies.py --check
    Check-Exit 'Native dependency receipts'
    $patchedMod = (& python tools/prepare_mygo.py).Trim()
    Check-Exit 'Prepare verified MyGo patch'
    $previousGoFlags = $env:GOFLAGS
    $previousLDFlags = $env:MYGO_LDFLAGS
    $previousRuntime = $env:GITOWL_RUNTIME_SMOKE
    $previousGoWork = $env:GOWORK
    try {
        $env:GOWORK = 'off'
        $env:GOFLAGS = '"-modfile=' + $patchedMod + '"'
        $env:MYGO_LDFLAGS = "-X main.BuildNumber=$BuildNumber"
        & npm.cmd run prepare:native-runtime
        Check-Exit 'Prepare checked runtime and licenses'
        $env:GITOWL_RUNTIME_SMOKE = Join-Path $root 'output/native-runtime/desktop-kernel'
        Push-Location app/native
        try {
            & go test ./...
            Check-Exit 'Native UI and transport tests'
            & go test github.com/egoist/mygo/cmd/mygo -run 'Test(GitowlInstallerProcessMessages|GitowlInstallerRemoval|NSISCloseApp|NSISLanguage|NSISSourceLanguage|NSISHooks)$' -count=1
            Check-Exit 'Installer language regression tests'
        } finally { Pop-Location }
        $package = Join-Path $root "_temp/native-packages/$BuildNumber"
        if (Test-Path $package) { throw 'Choose a new internal build number; package directories are immutable' }
        # Preparation/tests may not silently replace the accepted runtime. Check
        # again BEFORE mygo is allowed to create an installer.
        if (-not $Preview) {
            $acceptance = Invoke-NativeAcceptance
            if ($acceptance.sourceTree -ne $sourceTree) { throw 'Acceptance does not cover the staged packaging tree' }
        }
        $info = [ordered]@{
            product='Gitowl'; version='0.0.1'; build=$BuildNumber
            identifier='io.github.oubigfa.gitowl'; platform='windows-x64'
            sourceTree=$sourceTree; sourceSnapshot='staged Git inputs'
            authenticodeSigned=$false; fullFeatureParity=(-not $Preview)
            originalCore='desktop/desktop'; presentationComplete=(-not $Preview)
            runtimeInputHash=(Get-Content 'output/native-runtime/runtime-receipt.json' -Raw -Encoding UTF8 | ConvertFrom-Json).inputHash
            desktopAcceptance='pending'; installedAcceptance='pending'; channel='research-preview'
        }
        if (-not $Preview) {
            foreach ($property in $acceptance.PSObject.Properties) { $info[$property.Name] = $property.Value }
            $info['channel'] = 'fixed-test'
        }
        [IO.File]::WriteAllText((Join-Path $root 'output/native-runtime/build-info.json'), ($info | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
        Push-Location app/native
        try {
            & go tool mygo build -platform windows/amd64 -o $package
            Check-Exit 'MyGo Windows packaging'
        } finally { Pop-Location }
        & git diff --quiet
        Check-Exit 'Generated source differs from the staged build inputs'
        $finishedTree = (& git write-tree).Trim()
        Check-Exit 'Read completed source snapshot'
        if ($finishedTree -ne $sourceTree) { throw 'The staged source changed while packaging; do not publish this package' }
        if (-not $Preview) {
            $packaged = Invoke-NativeAcceptance "_temp/native-packages/$BuildNumber/windows-amd64"
            if ($packaged.sourceTree -ne $sourceTree) { throw 'Packaged acceptance does not cover the staged packaging tree' }
            $info['packagedExecutableSha256'] = $packaged.packagedExecutableSha256
        }
        $release = if ($Preview) { Join-Path $root "_temp/native-previews/$BuildNumber" } else { Join-Path $root 'output/release' }
        New-Item -ItemType Directory -Force $release | Out-Null
        $installer = Join-Path $package 'windows-amd64/gitowl-setup-0.0.1-amd64.exe'
        if (-not (Test-Path $installer)) { throw 'MyGo did not produce the expected installer' }
        $installerName = if ($Preview) { 'Gitowl-0.0.1-Preview-Setup.exe' } else { 'Gitowl-0.0.1-Setup.exe' }
        $fixed = Join-Path $release $installerName
        $checkedCopy = Join-Path $package 'checked-installer.exe'
        Copy-Item $installer $checkedCopy
        # Node can pass PowerShell 7's module paths to Windows PowerShell 5.1.
        # Hash the copy without depending on an incompatible Utility module.
        $hasher = [Security.Cryptography.SHA256]::Create()
        $stream = [IO.File]::OpenRead($checkedCopy)
        try {
            $hash = [BitConverter]::ToString($hasher.ComputeHash($stream)).Replace('-', '').ToLowerInvariant()
        } finally { $stream.Dispose(); $hasher.Dispose() }
        if (-not $Preview -and $hash -ne $packaged.installerSha256) { throw 'Installer changed after package validation; fixed installer was not touched' }
        Move-Item $checkedCopy $fixed -Force
        [IO.File]::WriteAllText(($fixed + '.sha256'), "$hash  $installerName`n", [Text.UTF8Encoding]::new($false))
        $info['installerSha256'] = $hash
        [IO.File]::WriteAllText((Join-Path $release 'build-info.json'), ($info | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
        Write-Output "Build $BuildNumber : $fixed"
        Write-Output "SHA-256: $hash"
        if ($Preview) {
            Write-Output 'Research preview only: no fixed delivery installer was updated; full acceptance is not certified.'
        } else {
            Write-Output 'Source-bound fixed test installer produced. Installed/upgrade acceptance is still required before delivery.'
        }
    } finally {
        $env:GOFLAGS = $previousGoFlags
        $env:MYGO_LDFLAGS = $previousLDFlags
        $env:GITOWL_RUNTIME_SMOKE = $previousRuntime
        $env:GOWORK = $previousGoWork
    }
} finally { Pop-Location }
