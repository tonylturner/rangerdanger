PROGRAM TapControl
  VAR
    Tap AT %IW100 : INT;
    DeviceFlags AT %IW101 : WORD;
    MeasurementValid AT %IW102 : UINT;
    GenerationHigh AT %IW109 : UINT;
    GenerationLow AT %IW110 : UINT;
    Voltage AT %IW112 : UINT;
    RequestResult AT %IW113 : UINT;
    DeviceHeartbeat AT %IW114 : UINT;
    Request AT %QW100 : UINT;
    Status AT %MW0 : WORD;
    Setpoint AT %MW1 : UINT;
    Deadband AT %MW2 : UINT;
    ConfiguredDelay AT %MW3 : UINT;
    LastDemand AT %MW4 : UINT;
    Heartbeat AT %MW5 : UINT;
  END_VAR
  VAR
    Remaining : UINT;
    Band : UDINT;
    DelayMS : UDINT;
    Stale : TON;
    Delay : TON;
    PreviousHeartbeat : UINT;
    PreviousGenerationHigh : UINT;
    PreviousGenerationLow : UINT;
    SeenHeartbeat : BOOL := FALSE;
    Changed : BOOL;
    ResetObserved : BOOL;
    Valid : BOOL;
    Permitted : BOOL;
    Direction : INT;
    PreviousDirection : INT;
    Pending : BOOL := FALSE;
    StartTap : INT;
    EncodedTarget : UINT;
    Scan : UINT := 0;
  END_VAR

  (* The task is 50 ms. Never retain requests or timer state across boots. *)
  Scan := Scan + 1;
  IF Scan >= 10 THEN
    Scan := 0;
    IF Heartbeat = 65535 THEN
      Heartbeat := 0;
    ELSE
      Heartbeat := Heartbeat + 1;
    END_IF;
  END_IF;

  Changed := DeviceHeartbeat <> PreviousHeartbeat;
  IF Changed THEN SeenHeartbeat := TRUE; END_IF;
  PreviousHeartbeat := DeviceHeartbeat;
  Stale(IN := NOT Changed, PT := T#2s);
  ResetObserved := (GenerationHigh <> PreviousGenerationHigh)
                   OR (GenerationLow <> PreviousGenerationLow);
  PreviousGenerationHigh := GenerationHigh;
  PreviousGenerationLow := GenerationLow;
  Valid := SeenHeartbeat AND NOT Stale.Q AND (MeasurementValid = 1)
           AND ((DeviceFlags AND WORD#16#0040) <> WORD#0);
  Permitted := Valid AND ((DeviceFlags AND WORD#16#0027) = WORD#16#0003)
               AND NOT ResetObserved AND (RequestResult <> 8);
  Status := WORD#1;
  IF Valid THEN Status := Status OR WORD#2; END_IF;
  IF Permitted THEN Status := Status OR WORD#4; END_IF;
  Setpoint := 20400;
  Deadband := 10;
  ConfiguredDelay := 30;
  Band := UINT_TO_UDINT(Setpoint) * UINT_TO_UDINT(Deadband) / 1000;
  DelayMS := UINT_TO_UDINT(ConfiguredDelay) * 1000;

  Direction := 0;
  IF Permitted THEN
    IF (UINT_TO_UDINT(Voltage) < (UINT_TO_UDINT(Setpoint) - Band)) AND (Tap < 8) THEN Direction := 1; END_IF;
    IF (UINT_TO_UDINT(Voltage) > (UINT_TO_UDINT(Setpoint) + Band)) AND (Tap > -8) THEN Direction := -1; END_IF;
  END_IF;
  IF Pending AND ((Tap <> StartTap) OR (Direction = 0)) THEN
    Pending := FALSE;
  END_IF;
  Delay(IN := (Direction <> 0) AND (Direction = PreviousDirection)
              AND NOT Pending, PT := UDINT_TO_TIME(DelayMS));
  PreviousDirection := Direction;
  Request := 0;
  Remaining := 0;
  IF Direction <> 0 THEN
    IF NOT Pending THEN
      Remaining := UDINT_TO_UINT((DelayMS - TIME_TO_UDINT(Delay.ET) + 999) / 1000);
      IF Delay.Q THEN
        StartTap := Tap;
        EncodedTarget := INT_TO_UINT(100 + Tap + Direction);
        Pending := TRUE;
        IF Direction = 1 THEN LastDemand := 2; ELSE LastDemand := 1; END_IF;
      END_IF;
    END_IF;
    IF Pending THEN Request := EncodedTarget; END_IF;
  END_IF;
END_PROGRAM

CONFIGURATION Config0
  RESOURCE Res0 ON PLC
    TASK Main(INTERVAL := T#50ms, PRIORITY := 0);
    PROGRAM Instance0 WITH Main : TapControl;
  END_RESOURCE
END_CONFIGURATION
