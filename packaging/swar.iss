; Windows installer for Swar, built by .github/workflows/build-desktop.yml.
#define AppVersion GetEnv("SWAR_VERSION")

[Setup]
AppId={{6E0B7C1E-5B2F-4F0D-9A51-2D3C5A7B9E11}
AppName=Swar
AppVersion={#AppVersion}
AppPublisher=Swar
DefaultDirName={autopf}\Swar
DefaultGroupName=Swar
DisableProgramGroupPage=yes
OutputDir=..\dist
OutputBaseFilename=Swar-Setup-{#AppVersion}
SetupIconFile=..\assets\icon\platform\windows\app_icon.ico
UninstallDisplayIcon={app}\Swar.exe
Compression=lzma2/ultra64
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequiredOverridesAllowed=dialog

[Files]
Source: "..\build\windows\x64\runner\Release\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\Swar"; Filename: "{app}\Swar.exe"
Name: "{autodesktop}\Swar"; Filename: "{app}\Swar.exe"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; Flags: unchecked

[Run]
Filename: "{app}\Swar.exe"; Description: "Start Swar"; Flags: nowait postinstall skipifsilent
