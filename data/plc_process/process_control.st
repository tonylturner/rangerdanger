PROGRAM process_control
  VAR
    pump_fb AT %IX0.0 : BOOL;
    sep_high AT %IX0.1 : BOOL;
    sep_low AT %IX0.2 : BOOL;
    oil_valve_fb AT %IX0.3 : BOOL;
    local_stop AT %IX0.6 : BOOL;
    pump_cmd AT %QX0.0 : BOOL;
    oil_valve AT %QX0.1 : BOOL;
    water_valve AT %QX0.2 : BOOL;
    gas_valve AT %QX0.3 : BOOL;
    running AT %QX0.5 : BOOL;
    manual_mode AT %QX0.6 : BOOL;
    sep_level AT %IW0 : INT;
    sep_pressure AT %IW1 : INT;
    oil_pos AT %QW0 : INT;
    pump_speed AT %QW2 : INT;
  END_VAR

  (* Safety interlock - always active regardless of mode *)
  IF local_stop THEN
    pump_cmd := FALSE;
    oil_valve := FALSE;
    water_valve := FALSE;
    gas_valve := FALSE;
    running := FALSE;
  ELSIF NOT manual_mode THEN
    (* Automatic mode - PLC controls outputs based on process *)
    IF sep_level < 30 THEN
      pump_cmd := TRUE;
      pump_speed := 100;
    ELSIF sep_level > 70 THEN
      pump_cmd := FALSE;
      pump_speed := 0;
    ELSE
      pump_cmd := TRUE;
      pump_speed := 50;
    END_IF;

    IF sep_high THEN
      pump_cmd := FALSE;
      pump_speed := 0;
      oil_valve := TRUE;
    END_IF;

    IF sep_level > 40 THEN
      oil_valve := TRUE;
      oil_pos := sep_level - 40;
    ELSE
      oil_valve := FALSE;
      oil_pos := 0;
    END_IF;

    IF sep_level > 50 THEN
      water_valve := TRUE;
    ELSE
      water_valve := FALSE;
    END_IF;

    gas_valve := pump_cmd OR (sep_level > 20);
  END_IF;

  (* Running status always calculated *)
  running := pump_fb OR pump_cmd;

END_PROGRAM

CONFIGURATION Config0
  RESOURCE Res0 ON PLC
    TASK Main(INTERVAL := T#50ms, PRIORITY := 0);
    PROGRAM Inst0 WITH Main : process_control;
  END_RESOURCE
END_CONFIGURATION
