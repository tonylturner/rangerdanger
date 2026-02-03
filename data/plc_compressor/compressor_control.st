PROGRAM compressor_control
  VAR
    comp_running AT %IX0.0 : BOOL;
    comp_ready AT %IX0.1 : BOOL;
    lube_ok AT %IX0.2 : BOOL;
    cooling_ok AT %IX0.3 : BOOL;
    vib_high AT %IX0.4 : BOOL;
    local_stop AT %IX0.7 : BOOL;
    comp_start AT %QX0.0 : BOOL;
    comp_stop AT %QX0.1 : BOOL;
    recycle AT %QX0.2 : BOOL;
    blowdown AT %QX0.3 : BOOL;
    fault AT %QX0.4 : BOOL;
    alarm AT %QX0.5 : BOOL;
    manual_mode AT %QX0.6 : BOOL;
    disch_press AT %IW1 : INT;
    flow AT %IW5 : INT;
    vibration AT %IW7 : INT;
    speed_sp AT %QW0 : INT;
    recycle_pos AT %QW1 : INT;
  END_VAR

  (* Safety interlocks - always active *)
  IF local_stop THEN
    comp_start := FALSE;
    comp_stop := TRUE;
    blowdown := TRUE;
    fault := TRUE;
  ELSIF NOT manual_mode THEN
    (* Automatic mode *)
    IF NOT comp_ready OR NOT lube_ok OR NOT cooling_ok THEN
      comp_start := FALSE;
      IF comp_running THEN
        comp_stop := TRUE;
        alarm := TRUE;
      END_IF;
    ELSIF vib_high OR vibration > 50 THEN
      comp_start := FALSE;
      comp_stop := TRUE;
      fault := TRUE;
      alarm := TRUE;
    ELSIF disch_press > 300 THEN
      recycle := TRUE;
      recycle_pos := 100;
      IF disch_press > 350 THEN
        comp_stop := TRUE;
        blowdown := TRUE;
      END_IF;
    ELSE
      recycle := FALSE;
      recycle_pos := 0;
    END_IF;

    IF flow < 500 AND comp_running THEN
      recycle := TRUE;
      recycle_pos := 100;
    END_IF;

    IF comp_running THEN
      IF disch_press < 240 THEN
        speed_sp := speed_sp + 50;
      ELSIF disch_press > 260 THEN
        speed_sp := speed_sp - 50;
      END_IF;
      IF speed_sp < 3000 THEN
        speed_sp := 3000;
      END_IF;
      IF speed_sp > 8000 THEN
        speed_sp := 8000;
      END_IF;
    END_IF;

    IF NOT comp_running AND comp_ready AND lube_ok AND cooling_ok THEN
      fault := FALSE;
      alarm := FALSE;
      blowdown := FALSE;
    END_IF;
  END_IF;

END_PROGRAM

CONFIGURATION Config0
  RESOURCE Res0 ON PLC
    TASK Main(INTERVAL := T#100ms, PRIORITY := 0);
    PROGRAM Inst0 WITH Main : compressor_control;
  END_RESOURCE
END_CONFIGURATION
