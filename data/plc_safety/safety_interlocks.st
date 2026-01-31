PROGRAM safety_interlocks
  (* SIS-301: Safety Instrumented System *)
  (* Emergency Shutdown and Safety Interlocks for Oil & Gas Facility *)
  (* SIL-2 rated logic - fail-safe design *)

  VAR
    (* INPUTS - Safety Sensors (2oo3 voting where applicable) *)
    esd_pb_1 AT %IX0.0 : BOOL;           (* ESD pushbutton #1 *)
    esd_pb_2 AT %IX0.1 : BOOL;           (* ESD pushbutton #2 *)
    fire_detect_1 AT %IX0.2 : BOOL;      (* Fire detector zone 1 *)
    fire_detect_2 AT %IX0.3 : BOOL;      (* Fire detector zone 2 *)
    gas_detect_1 AT %IX0.4 : BOOL;       (* Gas detector #1 *)
    gas_detect_2 AT %IX0.5 : BOOL;       (* Gas detector #2 *)
    gas_detect_3 AT %IX0.6 : BOOL;       (* Gas detector #3 *)
    high_high_press AT %IX0.7 : BOOL;    (* High-high pressure switch *)

    hi_hi_level AT %IX1.0 : BOOL;        (* High-high level switch *)
    lo_lo_level AT %IX1.1 : BOOL;        (* Low-low level switch *)
    vibration_trip AT %IX1.2 : BOOL;     (* Compressor vibration trip *)
    power_fail AT %IX1.3 : BOOL;         (* Power failure detect *)
    watchdog_fb AT %IX1.4 : BOOL;        (* Watchdog feedback *)
    bypass_key AT %IX1.5 : BOOL;         (* Maintenance bypass key *)
    reset_pb AT %IX1.6 : BOOL;           (* System reset pushbutton *)

    (* OUTPUTS - Safety Actions (energize-to-trip) *)
    esd_active AT %QX0.0 : BOOL;         (* ESD state active *)
    inlet_sdv AT %QX0.1 : BOOL;          (* Inlet shutdown valve CLOSE *)
    outlet_sdv AT %QX0.2 : BOOL;         (* Outlet shutdown valve CLOSE *)
    blowdown_valve AT %QX0.3 : BOOL;     (* Emergency blowdown OPEN *)
    comp_trip AT %QX0.4 : BOOL;          (* Compressor emergency trip *)
    pump_trip AT %QX0.5 : BOOL;          (* All pumps emergency trip *)
    alarm_horn AT %QX0.6 : BOOL;         (* Audible alarm *)
    beacon_red AT %QX0.7 : BOOL;         (* Red warning beacon *)

    deluge_zone1 AT %QX1.0 : BOOL;       (* Fire deluge zone 1 *)
    deluge_zone2 AT %QX1.1 : BOOL;       (* Fire deluge zone 2 *)
    hvac_shutdown AT %QX1.2 : BOOL;      (* HVAC shutdown for gas *)
    watchdog_out AT %QX1.3 : BOOL;       (* Watchdog pulse output *)

    (* ANALOG INPUTS - Safety Transmitters *)
    sep_pressure AT %IW0 : INT;          (* Separator pressure PSI *)
    sep_level AT %IW1 : INT;             (* Separator level % *)
    gas_conc_1 AT %IW2 : INT;            (* Gas concentration #1 %LEL *)
    gas_conc_2 AT %IW3 : INT;            (* Gas concentration #2 %LEL *)
    comp_vibration AT %IW4 : INT;        (* Compressor vibration mils *)

    (* INTERNAL VARIABLES *)
    esd_latched : BOOL := FALSE;
    fire_confirmed : BOOL;
    gas_high : BOOL;
    gas_high_high : BOOL;
    pressure_trip : BOOL;
    level_trip : BOOL;
    watchdog_timer : TON;
    wd_pulse : BOOL;
    reset_timer : TON;
    alarm_timer : TON;
    bypass_active : BOOL;
    trip_cause : INT;                    (* Cause code for diagnostics *)
  END_VAR

  (* Watchdog - toggle output to prove SIS is scanning *)
  watchdog_timer(IN := NOT wd_pulse, PT := T#500ms);
  IF watchdog_timer.Q THEN
    wd_pulse := NOT wd_pulse;
  END_IF;
  watchdog_out := wd_pulse;

  (* Bypass logic - only when key active, limited functions *)
  bypass_active := bypass_key;

  (* Gas detection - 2oo3 voting for high alarm *)
  gas_high := (gas_detect_1 AND gas_detect_2) OR
              (gas_detect_2 AND gas_detect_3) OR
              (gas_detect_1 AND gas_detect_3);

  (* Gas high-high from analog - any sensor > 50% LEL *)
  gas_high_high := (gas_conc_1 > 50) OR (gas_conc_2 > 50);

  (* Fire detection - any detector in either zone *)
  fire_confirmed := fire_detect_1 OR fire_detect_2;

  (* Pressure trip logic *)
  pressure_trip := high_high_press OR (sep_pressure > 450);

  (* Level trip logic *)
  level_trip := hi_hi_level OR lo_lo_level OR (sep_level > 95) OR (sep_level < 5);

  (* ESD Initiation - any of these conditions *)
  IF (esd_pb_1 OR esd_pb_2) AND NOT bypass_active THEN
    esd_latched := TRUE;
    trip_cause := 1;  (* Manual ESD *)
  END_IF;

  IF fire_confirmed AND NOT bypass_active THEN
    esd_latched := TRUE;
    trip_cause := 2;  (* Fire detected *)
  END_IF;

  IF gas_high_high AND NOT bypass_active THEN
    esd_latched := TRUE;
    trip_cause := 3;  (* Gas high-high *)
  END_IF;

  IF pressure_trip AND NOT bypass_active THEN
    esd_latched := TRUE;
    trip_cause := 4;  (* High pressure *)
  END_IF;

  IF vibration_trip OR (comp_vibration > 80) THEN
    esd_latched := TRUE;
    trip_cause := 5;  (* Compressor vibration *)
  END_IF;

  IF power_fail THEN
    esd_latched := TRUE;
    trip_cause := 6;  (* Power failure *)
  END_IF;

  (* ESD Actions when latched *)
  IF esd_latched THEN
    esd_active := TRUE;
    inlet_sdv := TRUE;       (* Close inlet *)
    outlet_sdv := TRUE;      (* Close outlet *)
    comp_trip := TRUE;       (* Trip compressor *)
    pump_trip := TRUE;       (* Trip pumps *)
    alarm_horn := TRUE;      (* Sound alarm *)
    beacon_red := TRUE;      (* Red beacon *)

    (* Blowdown on high pressure or gas *)
    IF pressure_trip OR gas_high_high THEN
      blowdown_valve := TRUE;
    END_IF;
  END_IF;

  (* Fire suppression - independent of ESD *)
  IF fire_detect_1 THEN
    deluge_zone1 := TRUE;
  END_IF;
  IF fire_detect_2 THEN
    deluge_zone2 := TRUE;
  END_IF;

  (* Gas alarm actions - HVAC shutdown *)
  IF gas_high OR gas_high_high THEN
    hvac_shutdown := TRUE;
    alarm_horn := TRUE;
  END_IF;

  (* Level trip actions - close valves, stop pumps *)
  IF level_trip AND NOT bypass_active THEN
    inlet_sdv := TRUE;
    pump_trip := TRUE;
  END_IF;

  (* Reset logic - requires held reset and all clear *)
  reset_timer(IN := reset_pb AND NOT fire_confirmed AND NOT gas_high_high
              AND NOT pressure_trip AND NOT level_trip AND NOT vibration_trip,
              PT := T#5s);

  IF reset_timer.Q THEN
    esd_latched := FALSE;
    esd_active := FALSE;
    inlet_sdv := FALSE;
    outlet_sdv := FALSE;
    blowdown_valve := FALSE;
    comp_trip := FALSE;
    pump_trip := FALSE;
    alarm_horn := FALSE;
    beacon_red := FALSE;
    deluge_zone1 := FALSE;
    deluge_zone2 := FALSE;
    hvac_shutdown := FALSE;
    trip_cause := 0;
  END_IF;

END_PROGRAM


CONFIGURATION Config0
  RESOURCE Res0 ON PLC
    (* Safety PLC runs faster scan for quicker response *)
    TASK SafetyMain(INTERVAL := T#20ms, PRIORITY := 0);
    PROGRAM Inst0 WITH SafetyMain : safety_interlocks;
  END_RESOURCE
END_CONFIGURATION
