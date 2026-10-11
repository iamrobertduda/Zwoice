#ifndef AppVersion
  #define AppVersion "1.1.0"
#endif
#ifndef SourceRoot
  #define SourceRoot "..\.."
#endif
[Setup]
AppId={{5F89586A-B408-4F7C-8B31-28F4B7993301}
AppName=Zwoice
AppVersion={#AppVersion}
AppPublisher=Zwoice
AppPublisherURL=https://github.com/iamrobertduda/Zwoice
DefaultDirName={localappdata}\Programs\Zwoice
DefaultGroupName=Zwoice
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible or arm64
ArchitecturesInstallIn64BitMode=x64compatible or arm64
DisableDirPage=yes
DisableProgramGroupPage=yes
DisableReadyPage=yes
WizardStyle=modern
Compression=lzma2
SolidCompression=yes
OutputDir={#SourceRoot}\dist
OutputBaseFilename=Zwoice-Windows-Setup
UninstallDisplayIcon={app}\Zwoice.exe
CloseApplications=yes
CloseApplicationsFilter=Zwoice.exe
[Languages]
Name: "german"; MessagesFile: "compiler:Languages\German.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"
[Files]
Source: "{#SourceRoot}\dist\windows-amd64\Zwoice.exe"; DestDir: "{app}"; Flags: ignoreversion; Check: not IsArm64
Source: "{#SourceRoot}\dist\windows-arm64\Zwoice.exe"; DestDir: "{app}"; Flags: ignoreversion; Check: IsArm64
Source: "{#SourceRoot}\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceRoot}\dist\GO-LICENSE.txt"; DestDir: "{app}"; Flags: ignoreversion
[Icons]
Name: "{autoprograms}\Zwoice"; Filename: "{app}\Zwoice.exe"
Name: "{userdesktop}\Zwoice"; Filename: "{app}\Zwoice.exe"
[Run]
Filename: "{app}\Zwoice.exe"; Description: "Zwoice starten"; Flags: nowait postinstall skipifsilent
