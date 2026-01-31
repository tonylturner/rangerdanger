PROGRAM process_control
  (* PLC-101: Process Control - Separator, Inlet Pump, Valves *)
  (* Oil & Gas Separation Unit Control Logic *)

  VAR
    (* INPUTS - Modbus %IX addresses *)
    inlet_pump_fb AT %IX0.0 : BOOL;      (* Inlet pump running feedback *)
    separator_high AT %IX0.1 : BOOL;     (* Separator high level switch *)
    separator_low AT %IX0.2 : BOOL;      (* Separator low level switch *)
    oil_valve_fb AT %IX0.3 : BOOL;       (* Oil outlet valve feedback *)
    water_valve_fb AT %IX0.4 : BOOL;     (* Water drain valve feedback *)
    gas_valve_fb AT %IX0.5 : BOOL;       (* Gas outlet valve feedback *)
    local_stop AT %IX0.6 : BOOL;         (* Local emergency stop *)
    remote_enable AT %IX0.7 : BOOL;      (* Remote enable from HMI *)

    (* OUTPUTS - Modbus %QX addresses *)
    inlet_pump_cmd AT %QX0.0 : BOOL;     (* Inlet pump start command *)
    oil_valve_cmd AT %QX0.1 : BOOL;      (* Oil outlet valve command *)
    water_valve_cmd AT %QX0.2 : BOOL;    (* Water drain valve command *)
    gas_valve_cmd AT %QX0.3 : BOOL;      (* Gas outlet valve command *)
    pump_fault AT %QX0.4 : BOOL;         (* Pump fault indicator *)
    process_running AT %QX0.5 : BOOL;    (* Process running status *)

    (* ANALOG INPUTS - Modbus %IW addresses *)
    separator_level AT %IW0 : INT;       (* Separator level 0-100% *)
    separator_pressure AT %IW1 : INT;    (* Separator pressure 0-500 PSI *)
    inlet_flow AT %IW2 : INT;            (* Inlet flow rate GPM *)
    oil_temp AT %IW3 : INT;              (* Oil temperature degF *)

    (* ANALOG OUTPUTS - Modbus %QW addresses *)
    oil_valve_pos AT %QW0 : INT;         (* Oil valve position setpoint *)
    water_valve_pos AT %QW1 : INT;       (* Water valve position setpoint *)
    pump_speed AT %QW2 : INT;            (* Pump VFD speed setpoint *)

    (* INTERNAL VARIABLES *)
    auto_mode : BOOL := TRUE;
    level_sp : INT := 50;                (* Level setpoint % *)
    level_error : INT;
    pump_timer : TON;
    fault_timer : TON;
  END_VAR

  (* Safety interlock - stop everything if local stop or no remote enable *)
  IF local_stop OR NOT remote_enable THEN
    inlet_pump_cmd := FALSE;
    oil_valve_cmd := FALSE;
    water_valve_cmd := FALSE;
    gas_valve_cmd := FALSE;
    process_running := FALSE;
    RETURN;
  END_IF;

  (* Calculate level error for control *)
  level_error := level_sp - separator_level;

  (* Inlet pump control - maintain separator level *)
  IF auto_mode THEN
    IF separator_level < 30 THEN
      inlet_pump_cmd := TRUE;
      pump_speed := 100;  (* Full speed when low *)
    ELSIF separator_level > 70 THEN
      inlet_pump_cmd := FALSE;
      pump_speed := 0;
    ELSE
      inlet_pump_cmd := TRUE;
      (* Modulate speed based on level *)
      pump_speed := 50 + level_error;
      IF pump_speed < 20 THEN pump_speed := 20; END_IF;
      IF pump_speed > 100 THEN pump_speed := 100; END_IF;
    END_IF;
  END_IF;

  (* High level protection *)
  IF separator_high THEN
    inlet_pump_cmd := FALSE;
    pump_speed := 0;
    oil_valve_cmd := TRUE;  (* Open to drain *)
  END_IF;

  (* Low level protection *)
  IF separator_low THEN
    oil_valve_cmd := FALSE;  (* Close to prevent gas breakthrough *)
    water_valve_cmd := FALSE;
  END_IF;

  (* Oil outlet valve control - proportional to level *)
  IF separator_level > 40 THEN
    oil_valve_cmd := TRUE;
    oil_valve_pos := (separator_level - 40) * 100 / 60;
    IF oil_valve_pos > 100 THEN oil_valve_pos := 100; END_IF;
  ELSE
    oil_valve_cmd := FALSE;
    oil_valve_pos := 0;
  END_IF;

  (* Water drain valve - timed drain cycle *)
  IF separator_level > 50 THEN
    water_valve_cmd := TRUE;
    water_valve_pos := 30;  (* Partial open for continuous drain *)
  ELSE
    water_valve_cmd := FALSE;
    water_valve_pos := 0;
  END_IF;

  (* Gas outlet always open when process running *)
  gas_valve_cmd := inlet_pump_cmd OR (separator_level > 20);

  (* Pump fault detection *)
  pump_timer(IN := inlet_pump_cmd AND NOT inlet_pump_fb, PT := T#5s);
  IF pump_timer.Q THEN
    pump_fault := TRUE;
    inlet_pump_cmd := FALSE;
  END_IF;

  (* Process running status *)
  process_running := inlet_pump_fb OR (separator_level > 10);

END_PROGRAM


CONFIGURATION Config0
  RESOURCE Res0 ON PLC
    TASK Main(INTERVAL := T#50ms, PRIORITY := 0);
    PROGRAM Inst0 WITH Main : process_control;
  END_RESOURCE
END_CONFIGURATION
