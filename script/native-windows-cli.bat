@echo off
setlocal
rem Use the original CLI parser; cli.js launches the single MyGo owner.
set ELECTRON_RUN_AS_NODE=1
call "%~dp0..\..\electron.exe" "%~dp0..\cli.js" %*
endlocal
