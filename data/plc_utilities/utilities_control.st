PROGRAM utilities_control
  VAR
    wt_pump_fb AT %IX0.0 : BOOL;
    wt_hi AT %IX0.2 : BOOL;
    wt_lo AT %IX0.3 : BOOL;
    chem_lo AT %IX0.4 : BOOL;
    oil_hi AT %IX0.6 : BOOL;
    oil_lo AT %IX0.7 : BOOL;
    truck AT %IX1.2 : BOOL;
    grounded AT %IX1.3 : BOOL;
    wt_pump AT %QX0.0 : BOOL;
    wt_inlet AT %QX0.1 : BOOL;
    chem_pump AT %QX0.3 : BOOL;
    wt_alarm AT %QX0.4 : BOOL;
    xfer_pump AT %QX0.6 : BOOL;
    tank_inlet AT %QX0.7 : BOOL;
    load_valve AT %QX1.0 : BOOL;
    load_permit AT %QX1.1 : BOOL;
    tank_alarm AT %QX1.2 : BOOL;
    manual_mode AT %QX1.3 : BOOL;
    wt_level AT %IW0 : INT;
    wt_ph AT %IW2 : INT;
    oil_level AT %IW3 : INT;
    chem_level AT %IW5 : INT;
    wt_speed AT %QW0 : INT;
    chem_rate AT %QW1 : INT;
    xfer_speed AT %QW2 : INT;
  END_VAR

  IF NOT manual_mode THEN
    (* Automatic mode - Water treatment control *)
    IF wt_level < 30 THEN
      wt_pump := TRUE;
      wt_inlet := TRUE;
      wt_speed := 100;
    ELSIF wt_level > 80 THEN
      wt_pump := FALSE;
      wt_inlet := FALSE;
      wt_speed := 0;
    ELSE
      wt_pump := TRUE;
      wt_inlet := TRUE;
      wt_speed := 50;
    END_IF;

    IF wt_hi OR wt_level > 95 THEN
      wt_pump := FALSE;
      wt_inlet := FALSE;
      wt_speed := 0;
    END_IF;

    IF wt_pump_fb AND NOT chem_lo THEN
      chem_pump := TRUE;
      IF wt_ph > 75 THEN
        chem_rate := 80;
      ELSIF wt_ph < 65 THEN
        chem_rate := 20;
      ELSE
        chem_rate := 50;
      END_IF;
    ELSE
      chem_pump := FALSE;
      chem_rate := 0;
    END_IF;

    IF chem_lo OR chem_level < 10 THEN
      wt_alarm := TRUE;
    END_IF;

    IF oil_level < 90 AND NOT oil_hi THEN
      tank_inlet := TRUE;
    ELSE
      tank_inlet := FALSE;
    END_IF;

    IF oil_hi OR oil_level > 95 THEN
      tank_inlet := FALSE;
      tank_alarm := TRUE;
    END_IF;

    IF oil_lo OR oil_level < 10 THEN
      tank_alarm := TRUE;
      xfer_pump := FALSE;
      load_valve := FALSE;
    END_IF;

    load_permit := truck AND grounded AND (oil_level > 20) AND NOT oil_lo;

    IF oil_level > 85 THEN
      xfer_pump := TRUE;
      xfer_speed := 50;
    ELSIF oil_level < 70 THEN
      xfer_pump := FALSE;
      xfer_speed := 0;
    END_IF;
  END_IF;

END_PROGRAM

CONFIGURATION Config0
  RESOURCE Res0 ON PLC
    TASK Main(INTERVAL := T#100ms, PRIORITY := 0);
    PROGRAM Inst0 WITH Main : utilities_control;
  END_RESOURCE
END_CONFIGURATION
