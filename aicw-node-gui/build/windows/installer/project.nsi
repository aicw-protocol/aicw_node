Unicode true

; Per-user install under the same folder the GUI already uses for node data.
!define REQUEST_EXECUTION_LEVEL "user"
!define INFO_PROJECTNAME "aicw-node-gui"
!define INFO_COMPANYNAME "AICW"
!define INFO_PRODUCTNAME "AICW Node"
!define INFO_PRODUCTVERSION "0.1.25"
!define INFO_COPYRIGHT "Copyright AICW"
!define PRODUCT_EXECUTABLE "aicw-node-setup.exe"
!define UNINST_KEY_NAME "AICW Node"

!include "wails_tools.nsh"

VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName" "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion" "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion" "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright" "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName" "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\aicw-node-setup-amd64-installer.exe"
InstallDir "$LOCALAPPDATA\Programs\AICW Node"
ShowInstDetails show

Function CloseAICWProcesses
 ; Ignore "process not found" from taskkill; it is not an install failure.
 nsExec::ExecToStack '"$SYSDIR\taskkill.exe" /F /IM aicw-node-setup.exe /T'
 Pop $0
 nsExec::ExecToStack '"$SYSDIR\taskkill.exe" /F /IM aicw-node.exe /T'
 Pop $0
 Sleep 1500
FunctionEnd

Function un.CloseAICWProcesses
 nsExec::ExecToStack '"$SYSDIR\taskkill.exe" /F /IM aicw-node-setup.exe /T'
 Pop $0
 nsExec::ExecToStack '"$SYSDIR\taskkill.exe" /F /IM aicw-node.exe /T'
 Pop $0
 Sleep 1500
FunctionEnd

Function .onInit
 !insertmacro wails.checkArchitecture
 Call CloseAICWProcesses
FunctionEnd

Section
 !insertmacro wails.setShellContext
 !insertmacro wails.webview2runtime

 Call CloseAICWProcesses
 SetOutPath $INSTDIR

 !insertmacro wails.files
 File "..\..\..\aicw-node.exe"

 CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
 CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

 !insertmacro wails.associateFiles
 !insertmacro wails.associateCustomProtocols

 Call WriteAICWUninstaller
SectionEnd

Function WriteAICWUninstaller
 WriteUninstaller "$INSTDIR\uninstall.exe"

 SetRegView 64
 WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
 WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
 WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
 WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
 WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
 WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"

 ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
 IntFmt $0 "0x%08X" $0
 WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
FunctionEnd

Section "uninstall"
 !insertmacro wails.setShellContext

 Call un.CloseAICWProcesses
 RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"
 RMDir /r $INSTDIR

 Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
 Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

 !insertmacro wails.unassociateFiles
 !insertmacro wails.unassociateCustomProtocols

 Delete "$INSTDIR\uninstall.exe"
 SetRegView 64
 DeleteRegKey HKCU "${UNINST_KEY}"
SectionEnd
