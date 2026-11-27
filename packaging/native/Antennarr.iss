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

; Keep the original identity and install location for in-place desktop upgrades.
[Setup]
AppId=Antennarr.Beta
AppName=Antennarr
AppVersion={#AppVersion}
AppPublisher=Antennarr
AppPublisherURL=https://github.com/fxPPC/Antennarr
ArchitecturesAllowed={#NativeArch}
ArchitecturesInstallIn64BitMode={#NativeArch}
PrivilegesRequired=lowest
DefaultDirName={localappdata}\Programs\Antennarr Beta
DefaultGroupName=Antennarr
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

[InstallDelete]
Type: files; Name: "{group}\Antennarr Beta.lnk"

[Icons]
Name: "{group}\Antennarr"; Filename: "{app}\Antennarr.exe"; IconFilename: "{app}\Antennarr.ico"

[Run]
Filename: "{app}\Antennarr.exe"; Description: "Start Antennarr"; Flags: postinstall nowait skipifsilent

[Code]
const
  RunKey = 'Software\Microsoft\Windows\CurrentVersion\Run';
  RunValue = 'Antennarr Beta';
  InstallerKey = 'Software\Antennarr\Beta\Installer';
  FirewallMarker = 'FirewallProgram';
  FirewallPorts = 'FirewallPorts';
  RuleName = 'Antennarr Beta LAN tuner';
var
  TasksInitialised, Upgrading: Boolean;
  PortPage: TInputQueryWizardPage;
  RangePath, InitialRange, TunerPorts: String;

function OwnsFirewallRule: Boolean; forward;

function LegacyPortSettings: Boolean;
begin
  Result := (GetEnv('ANTENNARR_LISTEN') <> '') or (GetEnv('ANTENNARR_ADMIN_LISTEN') <> '') or
            (GetEnv('ANTENNARR_TUNER_PORTS') <> '');
end;

function ParseRange(Value: String; var Tuners: String): Boolean;
var
  Split, FirstPort, LastPort: Integer;
begin
  Result := False;
  Split := Pos('-', Value);
  if Split = 0 then exit;
  FirstPort := StrToIntDef(Copy(Value, 1, Split - 1), -1);
  LastPort := StrToIntDef(Copy(Value, Split + 1, Length(Value)), -1);
  if (FirstPort < 1024) or (FirstPort > 65529) then exit;
  if (LastPort <> FirstPort + 6) or (Value <> IntToStr(FirstPort) + '-' + IntToStr(LastPort)) then exit;
  Tuners := IntToStr(FirstPort + 1) + '-' + IntToStr(LastPort);
  Result := True;
end;

procedure InitializeWizard;
var
  DataRoot: String;
  Lines: TArrayOfString;
begin
  DataRoot := GetEnv('ANTENNARR_DATA');
  if DataRoot = '' then DataRoot := ExpandConstant('{localappdata}\Antennarr Beta');
  RangePath := AddBackslash(ExpandFileName(DataRoot)) + 'config\port-range';
  InitialRange := GetEnv('ANTENNARR_PORT_RANGE');
  if (InitialRange = '') and not LegacyPortSettings and FileExists(RangePath) then begin
    if not LoadStringsFromFile(RangePath, Lines) or (GetArrayLength(Lines) <> 1) then
      RaiseException('Expected one port range in ' + RangePath);
    InitialRange := Trim(Lines[0]);
  end;
  if (InitialRange = '') and not LegacyPortSettings and
     not FileExists(ExtractFileDir(RangePath) + '\config.json') then
    InitialRange := '39192-39198';
  PortPage := CreateInputQueryPage(wpSelectDir, 'Network ports', 'Choose seven consecutive TCP ports.',
    'The first port is for local administration. The next six are for Plex tuners. ' +
    'An existing installation with legacy ports can leave this field blank to keep them.');
  PortPage.Add('Port range (for example, 39192-39198):', False);
  PortPage.Values[0] := ExpandConstant('{param:PORTRANGE|}');
  if PortPage.Values[0] = '' then PortPage.Values[0] := InitialRange;
end;

function RangeError(CheckFirewall: Boolean): String;
var
  Value, FromEnv: String;
begin
  Result := '';
  Value := Trim(PortPage.Values[0]);
  FromEnv := GetEnv('ANTENNARR_PORT_RANGE');
  TunerPorts := '';
  if (Value = '') and (InitialRange = '') then begin
    if CheckFirewall and WizardIsTaskSelected('lanfirewall') and not OwnsFirewallRule then
      Result := 'Choose a seven-port range to add a private-network tuner rule.';
  end else if not ParseRange(Value, TunerPorts) then
    Result := 'Choose seven consecutive ports from 1024 to 65535, such as 39192-39198.'
  else if (FromEnv <> '') and (FromEnv <> Value) then
    Result := 'Change ANTENNARR_PORT_RANGE first; it overrides the saved port range.'
  else if LegacyPortSettings then
    Result := 'Remove the legacy listener environment settings before selecting a seven-port range.';
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  Reason: String;
begin
  Result := True;
  if (CurPageID = PortPage.ID) or (CurPageID = wpSelectTasks) then begin
    Reason := RangeError(CurPageID = wpSelectTasks);
    Result := Reason = '';
    if not Result then SuppressibleMsgBox(Reason, mbError, MB_OK, IDOK);
  end;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := RangeError(True);
end;

function MoveFileEx(Source, Destination: String; Flags: Cardinal): Boolean;
  external 'MoveFileExW@kernel32.dll stdcall';

procedure SavePortRange;
var
  Value, Dir, Temp: String;
  Saved: AnsiString;
begin
  Value := Trim(PortPage.Values[0]);
  if (Value = '') or (GetEnv('ANTENNARR_PORT_RANGE') <> '') then exit;
  if LoadStringFromFile(RangePath, Saved) and (Trim(String(Saved)) = Value) then exit;
  Dir := ExtractFileDir(RangePath);
  if not ForceDirectories(Dir) then RaiseException('Cannot create ' + Dir);
  Temp := GenerateUniqueName(Dir, '.tmp');
  try
    if not SaveStringToFile(Temp, AnsiString(Value + #13#10), False) or
       not MoveFileEx(Temp, RangePath, 1) then RaiseException('Cannot save ' + RangePath);
  finally
    DeleteFile(Temp);
  end;
end;

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
  Args, Selector, SavedPorts: String;
  Code: Integer;
begin
  Selector := 'rule name="' + RuleName + '" program="' + ServerPath + '" dir=in';
  if AddRule then begin
    if OwnsFirewallRule then begin
      if (TunerPorts = '') or (RegQueryStringValue(HKCU, InstallerKey, FirewallPorts, SavedPorts) and
         (SavedPorts = TunerPorts)) then begin
        Result := True;
        exit;
      end;
      Args := 'set ' + Selector + ' new';
    end else
      Args := 'add ' + Selector;
    Args := Args + ' action=allow enable=yes profile=private remoteip=localsubnet' +
      ' protocol=TCP localport=' + TunerPorts;
  end else
    Args := 'delete ' + Selector;
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
    if AddRule then begin
      RegWriteStringValue(HKCU, InstallerKey, FirewallMarker, ServerPath);
      RegWriteStringValue(HKCU, InstallerKey, FirewallPorts, TunerPorts);
    end else begin
      RegDeleteValue(HKCU, InstallerKey, FirewallMarker);
      RegDeleteValue(HKCU, InstallerKey, FirewallPorts);
    end;
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
  SavePortRange();
  if Upgrading and WizardSilent and not ExplicitTasks then begin
    if OwnsFirewallRule and not ApplyFirewallRule(True) then
      RaiseException('Windows could not update the private-network tuner rule.');
    Log('Antennarr firewall and login choices preserved for silent upgrade.');
    exit;
  end;
  if WizardIsTaskSelected('startlogin') then
    RegWriteStringValue(HKCU, RunKey, RunValue, '"' + ExpandConstant('{app}\Antennarr.exe') + '" --login-item')
  else if OwnsLoginEntry then
    RegDeleteValue(HKCU, RunKey, RunValue);
  if WizardIsTaskSelected('lanfirewall') then begin
    Log('Antennarr firewall task selected.');
    if not ApplyFirewallRule(True) then
      SuppressibleMsgBox('Windows did not apply the private-network tuner rule. You can run Setup again to enable it.', mbError, MB_OK, IDOK);
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
