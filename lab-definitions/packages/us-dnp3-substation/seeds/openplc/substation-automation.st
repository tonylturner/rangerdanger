PROGRAM FeederInterlock
  VAR
    CloseRequest AT %MW0 : UINT;
    Status AT %MW1 : WORD;
    CloseOutput AT %MW2 : UINT;
    Heartbeat AT %MW3 : UINT;
  END_VAR
  VAR
    ScanCount : UINT := 0;
    BusVoltageOK : BOOL := TRUE;
    BreakerOpen : BOOL := TRUE;
    ProtectionHealthy : BOOL := TRUE;
    ClosePermissive : BOOL;
  END_VAR

  (* Local demonstration conditions only; no field I/O or remote polling. *)
  ScanCount := ScanCount + 1;
  IF ScanCount >= 10 THEN
    ScanCount := 0;
    IF Heartbeat = 65535 THEN
      Heartbeat := 0;
    ELSE
      Heartbeat := Heartbeat + 1;
    END_IF;
  END_IF;

  ClosePermissive := BreakerOpen AND BusVoltageOK AND ProtectionHealthy;
  Status := WORD#0;
  IF BreakerOpen THEN Status := Status OR WORD#1; END_IF;
  IF BusVoltageOK THEN Status := Status OR WORD#2; END_IF;
  IF ProtectionHealthy THEN Status := Status OR WORD#4; END_IF;
  IF ClosePermissive THEN Status := Status OR WORD#8; END_IF;

  CloseOutput := 0;
  IF (CloseRequest = 1) AND ClosePermissive THEN
    CloseOutput := 1;
    Status := Status OR WORD#16;
  END_IF;
END_PROGRAM

CONFIGURATION Config0
  RESOURCE Res0 ON PLC
    TASK Main(INTERVAL := T#50ms, PRIORITY := 0);
    PROGRAM Instance0 WITH Main : FeederInterlock;
  END_RESOURCE
END_CONFIGURATION
