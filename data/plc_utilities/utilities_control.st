PROGRAM utilities_control
  (* PLC-401: Utilities Control *)
  (* Water Treatment and Tank Farm Management *)

  VAR
    (* INPUTS - Water Treatment *)
    wt_pump_fb AT %IX0.0 : BOOL;         (* Water treatment pump running *)
    wt_filter_dp_high AT %IX0.1 : BOOL;  (* Filter diff pressure high *)
    wt_tank_high AT %IX0.2 : BOOL;       (* Treated water tank high *)
    wt_tank_low AT %IX0.3 : BOOL;        (* Treated water tank low *)
    chem_tank_low AT %IX0.4 : BOOL;      (* Chemical tank low *)
    wt_ph_alarm AT %IX0.5 : BOOL;        (* pH out of range *)

    (* INPUTS - Tank Farm *)
    oil_tank_high AT %IX0.6 : BOOL;      (* Oil storage tank high *)
    oil_tank_low AT %IX0.7 : BOOL;       (* Oil storage tank low *)
    transfer_pump_fb AT %IX1.0 : BOOL;   (* Transfer pump running *)
    loading_arm_ready AT %IX1.1 : BOOL;  (* Loading arm in position *)
    truck_present AT %IX1.2 : BOOL;      (* Truck at loading bay *)
    ground_connected AT %IX1.3 : BOOL;   (* Static ground connected *)

    (* OUTPUTS - Water Treatment *)
    wt_pump_cmd AT %QX0.0 : BOOL;        (* Water treatment pump start *)
    wt_inlet_valve AT %QX0.1 : BOOL;     (* WT inlet valve *)
    wt_outlet_valve AT %QX0.2 : BOOL;    (* WT outlet valve *)
    chem_pump_cmd AT %QX0.3 : BOOL;      (* Chemical dosing pump *)
    wt_alarm AT %QX0.4 : BOOL;           (* Water treatment alarm *)
    wt_running AT %QX0.5 : BOOL;         (* WT system running *)

    (* OUTPUTS - Tank Farm *)
    transfer_pump_cmd AT %QX0.6 : BOOL;  (* Transfer pump start *)
    tank_inlet_valve AT %QX0.7 : BOOL;   (* Tank inlet valve *)
    loading_valve AT %QX1.0 : BOOL;      (* Loading arm valve *)
    loading_permit AT %QX1.1 : BOOL;     (* Loading operation permit *)
    tank_alarm AT %QX1.2 : BOOL;         (* Tank farm alarm *)

    (* ANALOG INPUTS *)
    wt_tank_level AT %IW0 : INT;         (* Treated water tank % *)
    wt_flow AT %IW1 : INT;               (* Water treatment flow GPM *)
    wt_ph AT %IW2 : INT;                 (* pH x10 (70 = 7.0) *)
    oil_tank_level AT %IW3 : INT;        (* Oil storage tank % *)
    oil_temp AT %IW4 : INT;              (* Oil temperature degF *)
    chem_tank_level AT %IW5 : INT;       (* Chemical tank % *)
    loading_flow AT %IW6 : INT;          (* Loading flow rate GPM *)

    (* ANALOG OUTPUTS *)
    wt_pump_speed AT %QW0 : INT;         (* WT pump VFD speed % *)
    chem_dose_rate AT %QW1 : INT;        (* Chemical dose rate ml/min *)
    transfer_pump_speed AT %QW2 : INT;   (* Transfer pump speed % *)

    (* INTERNAL *)
    wt_auto_mode : BOOL := TRUE;
    tf_auto_mode : BOOL := TRUE;
    loading_active : BOOL;
    loading_timer : TON;
    pump_timer : TON;
    filter_timer : TON;
    ph_target : INT := 70;               (* Target pH 7.0 *)
  END_VAR

  (* ============ WATER TREATMENT LOGIC ============ *)

  (* Auto mode - maintain treated water tank level *)
  IF wt_auto_mode THEN
    IF wt_tank_level < 30 THEN
      wt_pump_cmd := TRUE;
      wt_inlet_valve := TRUE;
      wt_pump_speed := 100;
    ELSIF wt_tank_level > 80 THEN
      wt_pump_cmd := FALSE;
      wt_inlet_valve := FALSE;
      wt_pump_speed := 0;
    ELSIF wt_tank_level < 60 THEN
      wt_pump_cmd := TRUE;
      wt_inlet_valve := TRUE;
      (* Modulate speed based on level *)
      wt_pump_speed := 30 + (60 - wt_tank_level);
    END_IF;
  END_IF;

  (* High level protection *)
  IF wt_tank_high OR wt_tank_level > 95 THEN
    wt_pump_cmd := FALSE;
    wt_inlet_valve := FALSE;
    wt_pump_speed := 0;
  END_IF;

  (* Filter differential pressure alarm *)
  filter_timer(IN := wt_filter_dp_high, PT := T#10s);
  IF filter_timer.Q THEN
    wt_alarm := TRUE;
    (* Reduce flow but keep running *)
    IF wt_pump_speed > 50 THEN wt_pump_speed := 50; END_IF;
  END_IF;

  (* Chemical dosing control *)
  IF wt_pump_fb AND NOT chem_tank_low THEN
    chem_pump_cmd := TRUE;
    (* pH control - dose more if pH high, less if low *)
    IF wt_ph > ph_target + 5 THEN
      chem_dose_rate := 80;  (* High dose rate *)
    ELSIF wt_ph < ph_target - 5 THEN
      chem_dose_rate := 20;  (* Low dose rate *)
    ELSE
      chem_dose_rate := 50;  (* Normal dose *)
    END_IF;
  ELSE
    chem_pump_cmd := FALSE;
    chem_dose_rate := 0;
  END_IF;

  (* Chemical tank low alarm *)
  IF chem_tank_low OR chem_tank_level < 10 THEN
    wt_alarm := TRUE;
  END_IF;

  (* pH out of range alarm *)
  IF wt_ph_alarm OR wt_ph > 90 OR wt_ph < 50 THEN
    wt_alarm := TRUE;
  END_IF;

  (* Outlet valve follows pump *)
  wt_outlet_valve := wt_pump_fb;
  wt_running := wt_pump_fb;

  (* ============ TANK FARM LOGIC ============ *)

  (* Tank inlet valve - receive oil from separator *)
  IF oil_tank_level < 90 AND NOT oil_tank_high THEN
    tank_inlet_valve := TRUE;
  ELSE
    tank_inlet_valve := FALSE;
  END_IF;

  (* High level protection *)
  IF oil_tank_high OR oil_tank_level > 95 THEN
    tank_inlet_valve := FALSE;
    tank_alarm := TRUE;
  END_IF;

  (* Low level alarm *)
  IF oil_tank_low OR oil_tank_level < 10 THEN
    tank_alarm := TRUE;
    transfer_pump_cmd := FALSE;
    loading_valve := FALSE;
  END_IF;

  (* Loading operation permit *)
  (* All conditions must be met for safe loading *)
  loading_permit := truck_present AND
                    ground_connected AND
                    loading_arm_ready AND
                    oil_tank_level > 20 AND
                    NOT oil_tank_low;

  (* Loading sequence *)
  IF loading_permit AND loading_active THEN
    transfer_pump_cmd := TRUE;
    loading_valve := TRUE;
    transfer_pump_speed := 75;  (* Controlled rate *)

    (* Loading timeout - max 2 hours *)
    loading_timer(IN := TRUE, PT := T#7200s);
    IF loading_timer.Q THEN
      loading_active := FALSE;
    END_IF;
  ELSE
    IF NOT loading_permit THEN
      loading_active := FALSE;
    END_IF;
    loading_timer(IN := FALSE);
    loading_valve := FALSE;
    (* Keep transfer pump available for other operations *)
  END_IF;

  (* Transfer pump protection *)
  pump_timer(IN := transfer_pump_cmd AND NOT transfer_pump_fb, PT := T#5s);
  IF pump_timer.Q THEN
    transfer_pump_cmd := FALSE;
    tank_alarm := TRUE;
  END_IF;

  (* Temperature alarm - oil too hot or cold *)
  IF oil_temp > 150 OR oil_temp < 40 THEN
    tank_alarm := TRUE;
  END_IF;

  (* Auto transfer when tank gets full *)
  IF tf_auto_mode AND oil_tank_level > 85 AND NOT loading_active THEN
    transfer_pump_cmd := TRUE;
    transfer_pump_speed := 50;
  ELSIF oil_tank_level < 70 AND NOT loading_active THEN
    transfer_pump_cmd := FALSE;
    transfer_pump_speed := 0;
  END_IF;

END_PROGRAM


CONFIGURATION Config0
  RESOURCE Res0 ON PLC
    TASK Main(INTERVAL := T#100ms, PRIORITY := 0);
    PROGRAM Inst0 WITH Main : utilities_control;
  END_RESOURCE
END_CONFIGURATION
