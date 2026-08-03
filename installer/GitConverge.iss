#define AppName "GitConverge"
#ifndef AppVersion
  #define AppVersion "0.1.0-dev"
#endif
#ifndef BuildDir
  #define BuildDir "..\dist\windows-amd64"
#endif
#ifndef OutputDir
  #define OutputDir "..\dist"
#endif

[Setup]
AppId={{D26C7D46-73D4-4EA6-B535-2A56A38C1444}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher=GitConverge
DefaultDirName={localappdata}\Programs\GitConverge
DefaultGroupName=GitConverge
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={#OutputDir}
OutputBaseFilename=GitConverge-{#AppVersion}-windows-x64-setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
UninstallDisplayIcon={app}\gitconverge-tray.exe
LicenseFile=..\LICENSE
CloseApplications=yes
RestartApplications=no

[Files]
Source: "{#BuildDir}\gitconverge.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BuildDir}\gitconverge-tray.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\third_party_licenses\*"; DestDir: "{app}\third_party_licenses"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\GitConverge"; Filename: "{app}\gitconverge-tray.exe"

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "GitConverge"; ValueData: """{app}\gitconverge-tray.exe"""; Flags: uninsdeletevalue

[Run]
Filename: "{app}\gitconverge-tray.exe"; Description: "打开 GitConverge 并完成配置"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{cmd}"; Parameters: "/C schtasks /Delete /F /TN GitConverge"; Flags: runhidden waituntilterminated; RunOnceId: "DeleteTask"
Filename: "{cmd}"; Parameters: "/C taskkill /F /IM gitconverge-tray.exe"; Flags: runhidden waituntilterminated; RunOnceId: "StopTray"

[Code]
procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
begin
  if CurStep = ssInstall then
  begin
    Exec(ExpandConstant('{cmd}'), '/C taskkill /F /IM gitconverge-tray.exe', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;
end;
