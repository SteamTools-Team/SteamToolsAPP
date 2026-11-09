@echo off
setlocal

echo.
echo STEAMTOOLS BUILD SCRIPT (GO -^> Windows)
echo.

set "EXE=SteamTools.exe"

REM CLEANUP
echo [1/2] Cleaning up old files...
if exist "%EXE%" (
echo       - Removing old exe...
del /f /q "%EXE%"
)
echo.

REM BUILD
echo [2/2] Building Go binary (windows/amd64)...
echo       (this can take a while...)
echo.

set "GOOS=windows"
set "GOARCH=amd64"

go build -ldflags "-H=windowsgui" -o "%EXE%" .

if %ERRORLEVEL% EQU 0 (
echo       OK - Build succeeded
echo.
echo BUILD SUCCESSFUL
echo Executable: %EXE%
for %%A in ("%EXE%") do echo Size: %%~zA bytes

) else (
echo.
echo BUILD FAILED
echo Check the errors above
exit /b 1
)

endlocal
