; Build from the extracted native ZIP so portable and installed copies use the same binaries.
#ifndef AppVersion
  #error AppVersion is required (e.g. v1.1.0-beta.1)
#endif
#ifndef PayloadDir
  #error PayloadDir must point to the extracted Windows ZIP
#endif
#ifndef OutputDir
  #error OutputDir is required
#endif
#if AppArch == "amd64"
  #define NativeArch "x64os"
#elif AppArch == "arm64"
  #define NativeArch "arm64"
#else
  #error AppArch must be amd64 or arm64
#endif

[Setup]
AppId=Antennarr.Beta
AppName=Antennarr Beta
AppVersion={#AppVersion}
AppPublisher=Antennarr
AppPublisherURL=https://github.com/fxPPC/Antennarr
ArchitecturesAllowed={#NativeArch}
ArchitecturesInstallIn64BitMode={#NativeArch}
PrivilegesRequired=lowest
DefaultDirName={localappdata}\Programs\Antennarr Beta
DefaultGroupName=Antennarr Beta
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\Antennarr.ico
SetupIconFile={#PayloadDir}\Antennarr.ico
OutputDir={#OutputDir}
OutputBaseFilename=antennarr-{#AppVersion}-windows-{#AppArch}-setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
AppMutex=Local\Antennarr-Beta-Desktop
CloseApplications=yes
RestartApplications=no

[Messages]
SetupAppRunningError=%1 is running. Choose Quit Antennarr from its notification-area menu, then continue the installation.
UninstallAppRunningError=%1 is running. Choose Quit Antennarr from its notification-area menu, then continue uninstalling.

[Tasks]
Name: "startlogin"; Description: "Start Antennarr when I sign in"; Flags: unchecked
Name: "lanfirewall"; Description: "Allow Plex to reach the tuner on my private local network (requires administrator approval)"; Flags: unchecked

[Files]
Source: "{#PayloadDir}\*"; DestDir: "{app}"; Flags: recursesubdirs createallsubdirs ignoreversion

[Icons]
Name: "{group}\Antennarr Beta"; Filename: "{app}\Antennarr.exe"; IconFilename: "{app}\Antennarr.ico"

[Run]
Filename: "{app}\Antennarr.exe"; Description: "Start Antennarr Beta"; Flags: postinstall nowait skipifsilent

[Code]
const
  RunKey = 'Software\Microsoft\Windows\CurrentVersion\Run';
  RunValue = 'Antennarr Beta';
  InstallerKey = 'Software\Antennarr\Beta\Installer';
  FirewallMarker = 'FirewallProgram';
  RuleName = 'Antennarr Beta LAN tuner';
var
  TasksInitialised, Upgrading: Boolean;

function ExplicitTasks: Boolean;
var
  I: Integer;
  Arg: String;
begin
  Result := True;
  for I := 1 to ParamCount do begin
    Arg := Uppercase(ParamStr(I));
    if (Pos('/TASKS=', Arg) = 1) or (Pos('/MERGETASKS=', Arg) = 1) then exit;
  end;
  Result := False;
end;

function OwnsLoginEntry: Boolean;
var
  Value, Exe: String;
begin
  Exe := ExpandConstant('{app}\Antennarr.exe');
  Result := RegQueryStringValue(HKCU, RunKey, RunValue, Value) and
     ((CompareText(Value, '"' + Exe + '" --login-item') = 0) or
      (CompareText(Value, Exe + ' --login-item') = 0) or
      (CompareText(Value, '"' + Exe + '"') = 0) or (CompareText(Value, Exe) = 0));
end;

function ServerPath: String;
begin
  Result := ExpandConstant('{app}\antennarr-server.exe');
end;

function OwnsFirewallRule: Boolean;
var
  Value: String;
begin
  Result := RegQueryStringValue(HKCU, InstallerKey, FirewallMarker, Value) and
            (CompareText(Value, ServerPath) = 0);
end;

function ApplyFirewallRule(AddRule: Boolean): Boolean;
var
  Args: String;
  Code: Integer;
begin
  if AddRule then
    Args := 'add rule name="' + RuleName + '" program="' + ServerPath + '"' +
      ' dir=in action=allow enable=yes profile=private remoteip=localsubnet' +
      ' protocol=TCP localport=9192,9200-9207'
  else
    Args := 'delete rule name="' + RuleName + '" program="' + ServerPath + '" dir=in';
  Args := 'advfirewall firewall ' + Args;
  { A standard user elevates only the system tool. An already elevated
    installation, including unattended Setup, needs no second elevation. }
  if IsAdmin then
    Result := Exec(ExpandConstant('{sys}\netsh.exe'), Args, ExpandConstant('{sys}'),
      SW_HIDE, ewWaitUntilTerminated, Code)
  else
    Result := ShellExec('runas', ExpandConstant('{sys}\netsh.exe'), Args,
      ExpandConstant('{sys}'), SW_HIDE, ewWaitUntilTerminated, Code);
  Log(Format('Antennarr firewall: %s; netsh result %d', [Args, Code]));
  Result := Result and (Code = 0);
  if Result then begin
    if AddRule then
      RegWriteStringValue(HKCU, InstallerKey, FirewallMarker, ServerPath)
    else
      RegDeleteValue(HKCU, InstallerKey, FirewallMarker);
  end;
end;

procedure CurPageChanged(CurPageID: Integer);
var
  Tasks: String;
begin
  if (CurPageID <> wpSelectTasks) or TasksInitialised or ExplicitTasks then exit;
  TasksInitialised := True;
  if not FileExists(ExpandConstant('{app}\Antennarr.exe')) then exit;
  Tasks := '';
  if OwnsLoginEntry then Tasks := 'startlogin';
  if OwnsFirewallRule then begin
    if Tasks <> '' then Tasks := Tasks + ',';
    Tasks := Tasks + 'lanfirewall';
  end;
  WizardSelectTasks(Tasks);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssInstall then
    Upgrading := FileExists(ExpandConstant('{app}\Antennarr.exe'));
  if CurStep <> ssPostInstall then exit;
  if Upgrading and WizardSilent and not ExplicitTasks then begin
    Log('Antennarr firewall and login settings preserved for silent upgrade.');
    exit;
  end;
  if WizardIsTaskSelected('startlogin') then
    RegWriteStringValue(HKCU, RunKey, RunValue, '"' + ExpandConstant('{app}\Antennarr.exe') + '" --login-item')
  else if OwnsLoginEntry then
    RegDeleteValue(HKCU, RunKey, RunValue);
  if WizardIsTaskSelected('lanfirewall') then begin
    Log('Antennarr firewall task selected.');
    if not OwnsFirewallRule then
      if not ApplyFirewallRule(True) then
        SuppressibleMsgBox('Windows did not add the private-network tuner rule. You can run Setup again to enable it.', mbError, MB_OK, IDOK);
  end else if OwnsFirewallRule then
    if not ApplyFirewallRule(False) then
      SuppressibleMsgBox('Windows did not remove the Antennarr Beta LAN tuner rule. Remove it in Windows Defender Firewall.', mbError, MB_OK, IDOK);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep <> usUninstall then exit;
  if OwnsLoginEntry then RegDeleteValue(HKCU, RunKey, RunValue);
  if OwnsFirewallRule then
    if not ApplyFirewallRule(False) then
      SuppressibleMsgBox('Windows did not remove the Antennarr Beta LAN tuner rule. Remove it in Windows Defender Firewall.', mbError, MB_OK, IDOK);
  RegDeleteKeyIfEmpty(HKCU, InstallerKey);
end;
