PROGRAM compressor_control
  (* PLC-201: Compressor Control - Gas Compressor Unit *)
  (* Multi-stage gas compression with anti-surge protection *)

  VAR
    (* INPUTS - Modbus %IX addresses *)
    comp_running_fb AT %IX0.0 : BOOL;    (* Compressor running feedback *)
    comp_ready AT %IX0.1 : BOOL;         (* Compressor ready status *)
    lube_oil_ok AT %IX0.2 : BOOL;        (* Lube oil pressure OK *)
    cooling_ok AT %IX0.3 : BOOL;         (* Cooling water flow OK *)
    vibration_high AT %IX0.4 : BOOL;     (* High vibration alarm *)
    suction_low AT %IX0.5 : BOOL;        (* Low suction pressure *)
    discharge_high AT %IX0.6 : BOOL;     (* High discharge pressure *)
    local_stop AT %IX0.7 : BOOL;         (* Local emergency stop *)

    (* OUTPUTS - Modbus %QX addresses *)
    comp_start AT %QX0.0 : BOOL;         (* Compressor start command *)
    comp_stop AT %QX0.1 : BOOL;          (* Compressor stop command *)
    recycle_valve AT %QX0.2 : BOOL;      (* Anti-surge recycle valve *)
    blowdown_valve AT %QX0.3 : BOOL;     (* Emergency blowdown *)
    comp_fault AT %QX0.4 : BOOL;         (* Compressor fault status *)
    comp_alarm AT %QX0.5 : BOOL;         (* Compressor alarm *)
    anti_surge_active AT %QX0.6 : BOOL;  (* Anti-surge active *)

    (* ANALOG INPUTS - Modbus %IW addresses *)
    suction_pressure AT %IW0 : INT;      (* Suction pressure PSI *)
    discharge_pressure AT %IW1 : INT;    (* Discharge pressure PSI *)
    suction_temp AT %IW2 : INT;          (* Suction temperature degF *)
    discharge_temp AT %IW3 : INT;        (* Discharge temperature degF *)
    comp_speed AT %IW4 : INT;            (* Compressor speed RPM *)
    flow_rate AT %IW5 : INT;             (* Gas flow rate SCFM *)
    lube_oil_press AT %IW6 : INT;        (* Lube oil pressure PSI *)
    vibration AT %IW7 : INT;             (* Vibration level mils *)

    (* ANALOG OUTPUTS - Modbus %QW addresses *)
    speed_setpoint AT %QW0 : INT;        (* Speed setpoint RPM *)
    recycle_pos AT %QW1 : INT;           (* Recycle valve position % *)
    discharge_sp AT %QW2 : INT;          (* Discharge pressure SP *)

    (* INTERNAL VARIABLES *)
    surge_margin : INT;
    min_flow : INT := 500;               (* Minimum stable flow SCFM *)
    target_discharge : INT := 250;       (* Target discharge PSI *)
    pressure_ratio : INT;
    start_timer : TON;
    stop_timer : TON;
    run_request : BOOL;
    startup_complete : BOOL;
  END_VAR

  (* Emergency stop handling *)
  IF local_stop THEN
    comp_start := FALSE;
    comp_stop := TRUE;
    blowdown_valve := TRUE;
    comp_fault := TRUE;
    RETURN;
  END_IF;

  (* Permissives check *)
  IF NOT comp_ready OR NOT lube_oil_ok OR NOT cooling_ok THEN
    comp_start := FALSE;
    IF comp_running_fb THEN
      comp_stop := TRUE;
      comp_alarm := TRUE;
    END_IF;
    RETURN;
  END_IF;

  (* High vibration trip *)
  IF vibration_high OR vibration > 50 THEN
    comp_start := FALSE;
    comp_stop := TRUE;
    comp_fault := TRUE;
    comp_alarm := TRUE;
    RETURN;
  END_IF;

  (* Discharge overpressure protection *)
  IF discharge_high OR discharge_pressure > 300 THEN
    recycle_valve := TRUE;
    recycle_pos := 100;
    anti_surge_active := TRUE;
    IF discharge_pressure > 350 THEN
      comp_stop := TRUE;
      blowdown_valve := TRUE;
    END_IF;
  END_IF;

  (* Suction low pressure protection *)
  IF suction_low OR suction_pressure < 20 THEN
    comp_start := FALSE;
    IF comp_running_fb THEN
      (* Reduce speed, don't trip immediately *)
      speed_setpoint := speed_setpoint - 100;
      IF speed_setpoint < 1000 THEN
        comp_stop := TRUE;
      END_IF;
    END_IF;
  END_IF;

  (* Anti-surge control *)
  surge_margin := flow_rate - min_flow;
  IF surge_margin < 100 AND comp_running_fb THEN
    (* Approaching surge line - open recycle *)
    anti_surge_active := TRUE;
    recycle_valve := TRUE;
    recycle_pos := 100 - surge_margin;
    IF recycle_pos > 100 THEN recycle_pos := 100; END_IF;
    IF recycle_pos < 0 THEN recycle_pos := 0; END_IF;
  ELSE
    anti_surge_active := FALSE;
    recycle_valve := FALSE;
    recycle_pos := 0;
  END_IF;

  (* Pressure control - modulate speed *)
  IF comp_running_fb AND startup_complete THEN
    IF discharge_pressure < target_discharge - 10 THEN
      speed_setpoint := speed_setpoint + 50;
    ELSIF discharge_pressure > target_discharge + 10 THEN
      speed_setpoint := speed_setpoint - 50;
    END_IF;

    (* Speed limits *)
    IF speed_setpoint < 3000 THEN speed_setpoint := 3000; END_IF;
    IF speed_setpoint > 8000 THEN speed_setpoint := 8000; END_IF;
  END_IF;

  (* Startup sequence *)
  IF run_request AND NOT comp_running_fb AND NOT comp_fault THEN
    comp_stop := FALSE;
    start_timer(IN := TRUE, PT := T#3s);
    IF start_timer.Q THEN
      comp_start := TRUE;
      speed_setpoint := 3000;  (* Minimum speed *)
    END_IF;
  ELSE
    start_timer(IN := FALSE);
  END_IF;

  (* Ramp up after start confirmed *)
  IF comp_running_fb AND NOT startup_complete THEN
    start_timer(IN := TRUE, PT := T#30s);
    IF start_timer.Q THEN
      startup_complete := TRUE;
    END_IF;
    (* Gradual speed increase during startup *)
    speed_setpoint := 3000 + (comp_speed / 10);
  END_IF;

  (* Clear faults when stopped and conditions OK *)
  IF NOT comp_running_fb AND comp_ready AND lube_oil_ok AND cooling_ok THEN
    comp_fault := FALSE;
    comp_alarm := FALSE;
    blowdown_valve := FALSE;
  END_IF;

END_PROGRAM


CONFIGURATION Config0
  RESOURCE Res0 ON PLC
    TASK Main(INTERVAL := T#100ms, PRIORITY := 0);
    PROGRAM Inst0 WITH Main : compressor_control;
  END_RESOURCE
END_CONFIGURATION
