; Compile with /DAppVersion=... /DBundleDir=... /DOutputDir=...
#ifndef AppVersion
  #error AppVersion is required
#endif
#ifdef TestBuild
  #define ProductName "PortTide Installer Test"
#else
  #define ProductName "PortTide"
#endif
[Setup]
#ifdef TestBuild
AppId=PortTide-Isolated-Installer-Test
#else
AppId={{FF76949D-12B4-4B9E-9027-32C7E4B92B03}
#endif
AppName={#ProductName}
AppVersion={#AppVersion}
AppPublisher=irtez
AppPublisherURL=https://github.com/irtez/PortTide
DefaultDirName={localappdata}\Programs\PortTide
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
DefaultGroupName=PortTide
DisableProgramGroupPage=yes
OutputDir={#OutputDir}
OutputBaseFilename=PortTide-{#AppVersion}-windows-x64-setup
SetupIconFile=..\internal\app\tray.ico
UninstallDisplayIcon={app}\PortTide.ico
Compression=lzma2
SolidCompression=yes
CloseApplications=no
RestartApplications=no
WizardStyle=modern
UsePreviousTasks=yes

[Languages]
Name: english; MessagesFile: compiler:Default.isl
Name: russian; MessagesFile: compiler:Languages\Russian.isl

[Tasks]
Name: desktopicon; Description: "{cm:CreateDesktopIcon}"; Flags: unchecked
Name: startup; Description: "Запускать PortTide при входе в Windows"; Check: IsFirstInstall

[Files]
Source: "{#BundleDir}\PortTide.exe"; DestDir: "{tmp}"; DestName: "porttide-helper.exe"; Flags: dontcopy
Source: "{#BundleDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "..\internal\app\tray.ico"; DestDir: "{app}"; DestName: "PortTide.ico"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#ProductName}"; Filename: "{app}\PortTide.exe"; Parameters: "--open {code:DataArguments}"; IconFilename: "{app}\PortTide.ico"
Name: "{autodesktop}\{#ProductName}"; Filename: "{app}\PortTide.exe"; Parameters: "--open {code:DataArguments}"; IconFilename: "{app}\PortTide.ico"; Tasks: desktopicon

[INI]
Filename: "{app}\installed.txt"; Section: "PortTide"; Key: "installed"; String: "1"

[UninstallDelete]
Type: files; Name: "{app}\installed.txt"

[Code]
var Prepared, Completed, WasInstalled, RemoveData: Boolean;

function DataDirectory(Param: String): String;
begin
#ifdef TestBuild
  Result := '{#TestDataDir}';
#else
  Result := ExpandConstant('{localappdata}\PortTide');
#endif
end;

function DataArguments(Param: String): String;
begin
  Result := '--data-dir "' + DataDirectory('') + '"';
#ifdef TestBuild
  Result := Result + ' --listen 127.0.0.1:17794';
#endif
end;

function IsFirstInstall: Boolean;
begin
  Result := not FileExists(ExpandConstant('{app}\installed.txt'));
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var Code: Integer;
begin
  Result := '';
  if Prepared then exit;
  WasInstalled := not IsFirstInstall;
  ExtractTemporaryFile('porttide-helper.exe');
  if not Exec(ExpandConstant('{tmp}\porttide-helper.exe'), DataArguments('') + ' --no-browser --command prepare-update', '', SW_HIDE, ewWaitUntilTerminated, Code) or (Code <> 0) then
  begin
    Result := 'Не удалось остановить PortTide. Закройте приложение и повторите установку.';
    exit;
  end;
  Prepared := True;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var Code: Integer; Command: String;
begin
  if CurStep = ssPostInstall then
  begin
    Command := 'reconcile-login';
    if not WasInstalled then
    begin
      if WizardIsTaskSelected('startup') then Command := 'enable-login' else Command := 'disable-login';
    end;
    if not Exec(ExpandConstant('{app}\PortTide.exe'), DataArguments('') + ' --no-browser --command ' + Command, '', SW_HIDE, ewWaitUntilTerminated, Code) or (Code <> 0) then
      MsgBox('Не удалось настроить автозапуск. Проверьте его в настройках PortTide.', mbError, MB_OK);
  end;
  if CurStep = ssDone then
  begin
    Completed := True;
    if ExpandConstant('{param:NOLAUNCH|0}') = '1' then exit;
    if WasInstalled or (ExpandConstant('{param:UPDATE|0}') = '1') then Command := '--resume --no-browser' else Command := '--open';
    Exec(ExpandConstant('{app}\PortTide.exe'), DataArguments('') + ' ' + Command, '', SW_HIDE, ewNoWait, Code);
  end;
end;

procedure DeinitializeSetup;
var Code: Integer;
begin
  if Prepared and not Completed and WasInstalled and FileExists(ExpandConstant('{app}\PortTide.exe')) then
    Exec(ExpandConstant('{app}\PortTide.exe'), DataArguments('') + ' --resume --no-browser', '', SW_HIDE, ewNoWait, Code);
end;

function InitializeUninstall: Boolean;
var Code: Integer;
begin
  Result := Exec(ExpandConstant('{app}\PortTide.exe'), DataArguments('') + ' --no-browser --command shutdown', '', SW_HIDE, ewWaitUntilTerminated, Code) and (Code = 0);
  if not Result then begin MsgBox('Закройте PortTide и повторите удаление.', mbError, MB_OK); exit; end;
  RemoveData := False;
  if not UninstallSilent then
    RemoveData := MsgBox('Удалить также подписки, пароли и настройки PortTide? По умолчанию они сохраняются.', mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var Code: Integer;
begin
  if CurUninstallStep = usUninstall then
    Exec(ExpandConstant('{app}\PortTide.exe'), DataArguments('') + ' --no-browser --command disable-login', '', SW_HIDE, ewWaitUntilTerminated, Code);
  if (CurUninstallStep = usPostUninstall) and RemoveData then
    DelTree(DataDirectory(''), True, True, True);
end;
