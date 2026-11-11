; Run the original Desktop CLI registration helpers against this installation.
; MyGo already owns shortcuts, protocol registration and the uninstall entry.
!macro GitowlCLI event
  ClearErrors
  ExecWait '"$INSTDIR\desktop-kernel\electron.exe" "$INSTDIR\desktop-kernel\app" --gitowl-${event}-cli' $0
  IfErrors gitowl_cli_failed_${event}
  IntCmp $0 0 gitowl_cli_done_${event}
  gitowl_cli_failed_${event}:
    SetErrorLevel 1
    Abort
  gitowl_cli_done_${event}:
!macroend

!macro customInstall
  !insertmacro GitowlCLI install
!macroend

!macro customUnInstall
  !insertmacro GitowlCLI uninstall
!macroend
