PROGRAM safety_interlocks
  VAR
    esd_pb1 AT %IX0.0 : BOOL;
    esd_pb2 AT %IX0.1 : BOOL;
    fire1 AT %IX0.2 : BOOL;
    fire2 AT %IX0.3 : BOOL;
    gas1 AT %IX0.4 : BOOL;
    gas2 AT %IX0.5 : BOOL;
    gas3 AT %IX0.6 : BOOL;
    hihi_press AT %IX0.7 : BOOL;
    hihi_level AT %IX1.0 : BOOL;
    lolo_level AT %IX1.1 : BOOL;
    vib_trip AT %IX1.2 : BOOL;
    bypass_sw AT %IX1.5 : BOOL;
    reset_pb AT %IX1.6 : BOOL;
    esd_active AT %QX0.0 : BOOL;
    inlet_sdv AT %QX0.1 : BOOL;
    outlet_sdv AT %QX0.2 : BOOL;
    blowdown AT %QX0.3 : BOOL;
    comp_trip AT %QX0.4 : BOOL;
    pump_trip AT %QX0.5 : BOOL;
    horn AT %QX0.6 : BOOL;
    beacon AT %QX0.7 : BOOL;
    deluge1 AT %QX1.0 : BOOL;
    deluge2 AT %QX1.1 : BOOL;
    sep_press AT %IW0 : INT;
    gas_ppm1 AT %IW2 : INT;
    gas_ppm2 AT %IW3 : INT;
  END_VAR
  VAR
    gas_high : BOOL;
    gas_hihi : BOOL;
    esd_latch : BOOL;
  END_VAR

  gas_high := (gas1 AND gas2) OR (gas2 AND gas3) OR (gas1 AND gas3);
  gas_hihi := (gas_ppm1 > 50) OR (gas_ppm2 > 50);

  IF (esd_pb1 OR esd_pb2) AND NOT bypass_sw THEN
    esd_latch := TRUE;
  END_IF;

  IF (fire1 OR fire2) AND NOT bypass_sw THEN
    esd_latch := TRUE;
  END_IF;

  IF gas_hihi AND NOT bypass_sw THEN
    esd_latch := TRUE;
  END_IF;

  IF (hihi_press OR sep_press > 450) AND NOT bypass_sw THEN
    esd_latch := TRUE;
  END_IF;

  IF vib_trip THEN
    esd_latch := TRUE;
  END_IF;

  IF esd_latch THEN
    esd_active := TRUE;
    inlet_sdv := TRUE;
    outlet_sdv := TRUE;
    comp_trip := TRUE;
    pump_trip := TRUE;
    horn := TRUE;
    beacon := TRUE;
  END_IF;

  IF hihi_press OR gas_hihi THEN
    blowdown := TRUE;
  END_IF;

  IF fire1 THEN
    deluge1 := TRUE;
  END_IF;

  IF fire2 THEN
    deluge2 := TRUE;
  END_IF;

  IF (hihi_level OR lolo_level) AND NOT bypass_sw THEN
    inlet_sdv := TRUE;
    pump_trip := TRUE;
  END_IF;

  IF reset_pb THEN
    IF NOT fire1 THEN
      IF NOT fire2 THEN
        IF NOT gas_hihi THEN
          IF NOT hihi_press THEN
            IF NOT vib_trip THEN
              esd_latch := FALSE;
              esd_active := FALSE;
              inlet_sdv := FALSE;
              outlet_sdv := FALSE;
              blowdown := FALSE;
              comp_trip := FALSE;
              pump_trip := FALSE;
              horn := FALSE;
              beacon := FALSE;
              deluge1 := FALSE;
              deluge2 := FALSE;
            END_IF;
          END_IF;
        END_IF;
      END_IF;
    END_IF;
  END_IF;

END_PROGRAM

CONFIGURATION Config0
  RESOURCE Res0 ON PLC
    TASK Main(INTERVAL := T#20ms, PRIORITY := 0);
    PROGRAM Inst0 WITH Main : safety_interlocks;
  END_RESOURCE
END_CONFIGURATION
