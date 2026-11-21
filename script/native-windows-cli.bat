@echo off
setlocal
rem Use the original CLI parser; cli.js launches the single MyGo owner.
call "%~dp0..\node.exe" "%~dp0..\cli.js" %*
endlocal
