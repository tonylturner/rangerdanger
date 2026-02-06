#!/usr/bin/env python3
"""Configure FUXA HMI databases for RangerDanger OT Cyber Range."""

import json
import sqlite3
import os
import shutil

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# Device configurations with full tag definitions
DEVICES = {
    "plc101": {
        "id": "plc101",
        "name": "PLC-101 Process",
        "type": "ModbusTCP",
        "property": {
            "address": "10.30.30.20",
            "port": 502,
            "slaveid": 1,
            "delay": 0,
            "options": {}
        },
        "enabled": True,
        "polling": 1000,
        "tags": {
            # Input bits (IX) - read with FC2 (Discrete Inputs) - FUXA uses 1-based addressing
            "pump_fb": {"id": "pump_fb", "name": "Pump Feedback", "address": "1", "type": "Bool", "memaddress": "100000", "readonly": True},
            "sep_high": {"id": "sep_high", "name": "Separator High", "address": "2", "type": "Bool", "memaddress": "100000", "readonly": True},
            "sep_low": {"id": "sep_low", "name": "Separator Low", "address": "3", "type": "Bool", "memaddress": "100000", "readonly": True},
            "oil_valve_fb": {"id": "oil_valve_fb", "name": "Oil Valve FB", "address": "4", "type": "Bool", "memaddress": "100000", "readonly": True},
            "local_stop": {"id": "local_stop", "name": "Local Stop", "address": "7", "type": "Bool", "memaddress": "100000", "readonly": True},
            # Output bits (QX) - read/write with FC1/5 (Coils) - FUXA uses 1-based addressing
            "pump_cmd": {"id": "pump_cmd", "name": "Pump Command", "address": "1", "type": "Bool", "memaddress": "000000"},
            "oil_valve": {"id": "oil_valve", "name": "Oil Valve", "address": "2", "type": "Bool", "memaddress": "000000"},
            "water_valve": {"id": "water_valve", "name": "Water Valve", "address": "3", "type": "Bool", "memaddress": "000000"},
            "gas_valve": {"id": "gas_valve", "name": "Gas Valve", "address": "4", "type": "Bool", "memaddress": "000000"},
            "running": {"id": "running", "name": "Running", "address": "6", "type": "Bool", "memaddress": "000000", "readonly": True},
            "manual_mode": {"id": "manual_mode", "name": "Manual Mode", "address": "7", "type": "Bool", "memaddress": "000000"},
            # Input words (IW) - read with FC4 (Input Registers) - FUXA uses 1-based addressing
            "sep_level": {"id": "sep_level", "name": "Separator Level", "address": "1", "type": "Int16", "memaddress": "300000", "readonly": True},
            "sep_pressure": {"id": "sep_pressure", "name": "Separator Pressure", "address": "2", "type": "Int16", "memaddress": "300000", "readonly": True},
            # Output words (QW) - read/write with FC3/6 (Holding Registers) - FUXA uses 1-based addressing
            "oil_pos": {"id": "oil_pos", "name": "Oil Valve Position", "address": "1", "type": "Int16", "memaddress": "400000"},
            "pump_speed": {"id": "pump_speed", "name": "Pump Speed", "address": "3", "type": "Int16", "memaddress": "400000"},
        }
    },
    "plc201": {
        "id": "plc201",
        "name": "PLC-201 Compressor",
        "type": "ModbusTCP",
        "property": {
            "address": "10.30.30.21",
            "port": 502,
            "slaveid": 1,
            "delay": 0,
            "options": {}
        },
        "enabled": True,
        "polling": 1000,
        "tags": {
            # Input bits - FUXA uses 1-based addressing
            "comp_running": {"id": "comp_running", "name": "Compressor Running", "address": "1", "type": "Bool", "memaddress": "100000", "readonly": True},
            "comp_ready": {"id": "comp_ready", "name": "Compressor Ready", "address": "2", "type": "Bool", "memaddress": "100000", "readonly": True},
            "lube_ok": {"id": "lube_ok", "name": "Lube OK", "address": "3", "type": "Bool", "memaddress": "100000", "readonly": True},
            "cooling_ok": {"id": "cooling_ok", "name": "Cooling OK", "address": "4", "type": "Bool", "memaddress": "100000", "readonly": True},
            "vib_high": {"id": "vib_high", "name": "Vibration High", "address": "5", "type": "Bool", "memaddress": "100000", "readonly": True},
            "local_stop": {"id": "local_stop", "name": "Local Stop", "address": "8", "type": "Bool", "memaddress": "100000", "readonly": True},
            # Output bits - FUXA uses 1-based addressing
            "comp_start": {"id": "comp_start", "name": "Compressor Start", "address": "1", "type": "Bool", "memaddress": "000000"},
            "comp_stop": {"id": "comp_stop", "name": "Compressor Stop", "address": "2", "type": "Bool", "memaddress": "000000"},
            "recycle": {"id": "recycle", "name": "Recycle Valve", "address": "3", "type": "Bool", "memaddress": "000000"},
            "blowdown": {"id": "blowdown", "name": "Blowdown", "address": "4", "type": "Bool", "memaddress": "000000"},
            "fault": {"id": "fault", "name": "Fault", "address": "5", "type": "Bool", "memaddress": "000000", "readonly": True},
            "alarm": {"id": "alarm", "name": "Alarm", "address": "6", "type": "Bool", "memaddress": "000000", "readonly": True},
            "manual_mode": {"id": "manual_mode", "name": "Manual Mode", "address": "7", "type": "Bool", "memaddress": "000000"},
            # Input words - FUXA uses 1-based addressing
            "disch_press": {"id": "disch_press", "name": "Discharge Pressure", "address": "2", "type": "Int16", "memaddress": "300000", "readonly": True},
            "flow": {"id": "flow", "name": "Flow", "address": "6", "type": "Int16", "memaddress": "300000", "readonly": True},
            "vibration": {"id": "vibration", "name": "Vibration", "address": "8", "type": "Int16", "memaddress": "300000", "readonly": True},
            # Output words - FUXA uses 1-based addressing
            "speed_sp": {"id": "speed_sp", "name": "Speed Setpoint", "address": "1", "type": "Int16", "memaddress": "400000"},
            "recycle_pos": {"id": "recycle_pos", "name": "Recycle Position", "address": "2", "type": "Int16", "memaddress": "400000"},
        }
    },
    "sis301": {
        "id": "sis301",
        "name": "SIS-301 Safety",
        "type": "ModbusTCP",
        "property": {
            "address": "10.40.40.20",
            "port": 502,
            "slaveid": 1,
            "delay": 0,
            "options": {}
        },
        "enabled": True,
        "polling": 500,
        "tags": {
            # Input bits - all safety inputs - FUXA uses 1-based addressing
            "esd_pb1": {"id": "esd_pb1", "name": "ESD Button 1", "address": "1", "type": "Bool", "memaddress": "100000", "readonly": True},
            "esd_pb2": {"id": "esd_pb2", "name": "ESD Button 2", "address": "2", "type": "Bool", "memaddress": "100000", "readonly": True},
            "fire1": {"id": "fire1", "name": "Fire Detector 1", "address": "3", "type": "Bool", "memaddress": "100000", "readonly": True},
            "fire2": {"id": "fire2", "name": "Fire Detector 2", "address": "4", "type": "Bool", "memaddress": "100000", "readonly": True},
            "gas1": {"id": "gas1", "name": "Gas Detector 1", "address": "5", "type": "Bool", "memaddress": "100000", "readonly": True},
            "gas2": {"id": "gas2", "name": "Gas Detector 2", "address": "6", "type": "Bool", "memaddress": "100000", "readonly": True},
            "gas3": {"id": "gas3", "name": "Gas Detector 3", "address": "7", "type": "Bool", "memaddress": "100000", "readonly": True},
            "hihi_press": {"id": "hihi_press", "name": "HiHi Pressure", "address": "8", "type": "Bool", "memaddress": "100000", "readonly": True},
            "hihi_level": {"id": "hihi_level", "name": "HiHi Level", "address": "9", "type": "Bool", "memaddress": "100000", "readonly": True},
            "lolo_level": {"id": "lolo_level", "name": "LoLo Level", "address": "10", "type": "Bool", "memaddress": "100000", "readonly": True},
            "vib_trip": {"id": "vib_trip", "name": "Vibration Trip", "address": "11", "type": "Bool", "memaddress": "100000", "readonly": True},
            "bypass_sw": {"id": "bypass_sw", "name": "Bypass Switch", "address": "14", "type": "Bool", "memaddress": "100000", "readonly": True},
            "reset_pb": {"id": "reset_pb", "name": "Reset Button", "address": "15", "type": "Bool", "memaddress": "100000", "readonly": True},
            # Output bits - safety outputs (CRITICAL - should be protected!) - FUXA uses 1-based addressing
            "esd_active": {"id": "esd_active", "name": "ESD Active", "address": "1", "type": "Bool", "memaddress": "000000", "readonly": True},
            "inlet_sdv": {"id": "inlet_sdv", "name": "Inlet SDV", "address": "2", "type": "Bool", "memaddress": "000000", "readonly": True},
            "outlet_sdv": {"id": "outlet_sdv", "name": "Outlet SDV", "address": "3", "type": "Bool", "memaddress": "000000", "readonly": True},
            "blowdown": {"id": "blowdown", "name": "Blowdown", "address": "4", "type": "Bool", "memaddress": "000000", "readonly": True},
            "comp_trip": {"id": "comp_trip", "name": "Compressor Trip", "address": "5", "type": "Bool", "memaddress": "000000", "readonly": True},
            "pump_trip": {"id": "pump_trip", "name": "Pump Trip", "address": "6", "type": "Bool", "memaddress": "000000", "readonly": True},
            "horn": {"id": "horn", "name": "Horn", "address": "7", "type": "Bool", "memaddress": "000000", "readonly": True},
            "beacon": {"id": "beacon", "name": "Beacon", "address": "8", "type": "Bool", "memaddress": "000000", "readonly": True},
            "deluge1": {"id": "deluge1", "name": "Deluge Zone 1", "address": "9", "type": "Bool", "memaddress": "000000", "readonly": True},
            "deluge2": {"id": "deluge2", "name": "Deluge Zone 2", "address": "10", "type": "Bool", "memaddress": "000000", "readonly": True},
            # Input words - FUXA uses 1-based addressing
            "sep_press": {"id": "sep_press", "name": "Separator Pressure", "address": "1", "type": "Int16", "memaddress": "300000", "readonly": True},
            "gas_ppm1": {"id": "gas_ppm1", "name": "Gas PPM Sensor 1", "address": "3", "type": "Int16", "memaddress": "300000", "readonly": True},
            "gas_ppm2": {"id": "gas_ppm2", "name": "Gas PPM Sensor 2", "address": "4", "type": "Int16", "memaddress": "300000", "readonly": True},
        }
    },
    "plc401": {
        "id": "plc401",
        "name": "PLC-401 Utilities",
        "type": "ModbusTCP",
        "property": {
            "address": "10.30.30.22",
            "port": 502,
            "slaveid": 1,
            "delay": 0,
            "options": {}
        },
        "enabled": True,
        "polling": 1000,
        "tags": {
            # Input bits - FUXA uses 1-based addressing
            "wt_pump_fb": {"id": "wt_pump_fb", "name": "WT Pump Feedback", "address": "1", "type": "Bool", "memaddress": "100000", "readonly": True},
            "wt_hi": {"id": "wt_hi", "name": "WT High Level", "address": "3", "type": "Bool", "memaddress": "100000", "readonly": True},
            "wt_lo": {"id": "wt_lo", "name": "WT Low Level", "address": "4", "type": "Bool", "memaddress": "100000", "readonly": True},
            "chem_lo": {"id": "chem_lo", "name": "Chemical Low", "address": "5", "type": "Bool", "memaddress": "100000", "readonly": True},
            "oil_hi": {"id": "oil_hi", "name": "Oil Tank High", "address": "7", "type": "Bool", "memaddress": "100000", "readonly": True},
            "oil_lo": {"id": "oil_lo", "name": "Oil Tank Low", "address": "8", "type": "Bool", "memaddress": "100000", "readonly": True},
            "truck": {"id": "truck", "name": "Truck Present", "address": "11", "type": "Bool", "memaddress": "100000", "readonly": True},
            "grounded": {"id": "grounded", "name": "Truck Grounded", "address": "12", "type": "Bool", "memaddress": "100000", "readonly": True},
            # Output bits - FUXA uses 1-based addressing
            "wt_pump": {"id": "wt_pump", "name": "WT Pump", "address": "1", "type": "Bool", "memaddress": "000000"},
            "wt_inlet": {"id": "wt_inlet", "name": "WT Inlet Valve", "address": "2", "type": "Bool", "memaddress": "000000"},
            "chem_pump": {"id": "chem_pump", "name": "Chemical Pump", "address": "4", "type": "Bool", "memaddress": "000000"},
            "wt_alarm": {"id": "wt_alarm", "name": "WT Alarm", "address": "5", "type": "Bool", "memaddress": "000000", "readonly": True},
            "xfer_pump": {"id": "xfer_pump", "name": "Transfer Pump", "address": "7", "type": "Bool", "memaddress": "000000"},
            "tank_inlet": {"id": "tank_inlet", "name": "Tank Inlet", "address": "8", "type": "Bool", "memaddress": "000000"},
            "load_valve": {"id": "load_valve", "name": "Load Valve", "address": "9", "type": "Bool", "memaddress": "000000"},
            "load_permit": {"id": "load_permit", "name": "Load Permit", "address": "10", "type": "Bool", "memaddress": "000000", "readonly": True},
            "tank_alarm": {"id": "tank_alarm", "name": "Tank Alarm", "address": "11", "type": "Bool", "memaddress": "000000", "readonly": True},
            "manual_mode": {"id": "manual_mode", "name": "Manual Mode", "address": "12", "type": "Bool", "memaddress": "000000"},
            # Input words - FUXA uses 1-based addressing
            "wt_level": {"id": "wt_level", "name": "WT Level", "address": "1", "type": "Int16", "memaddress": "300000", "readonly": True},
            "wt_ph": {"id": "wt_ph", "name": "WT pH", "address": "3", "type": "Int16", "memaddress": "300000", "readonly": True},
            "oil_level": {"id": "oil_level", "name": "Oil Tank Level", "address": "4", "type": "Int16", "memaddress": "300000", "readonly": True},
            "chem_level": {"id": "chem_level", "name": "Chemical Level", "address": "6", "type": "Int16", "memaddress": "300000", "readonly": True},
            # Output words - FUXA uses 1-based addressing
            "wt_speed": {"id": "wt_speed", "name": "WT Pump Speed", "address": "1", "type": "Int16", "memaddress": "400000"},
            "chem_rate": {"id": "chem_rate", "name": "Chemical Rate", "address": "2", "type": "Int16", "memaddress": "400000"},
            "xfer_speed": {"id": "xfer_speed", "name": "Transfer Speed", "address": "3", "type": "Int16", "memaddress": "400000"},
        }
    }
}

# FUXA Server device (always present)
FUXA_SERVER = {
    "id": "0",
    "name": "FUXA Server",
    "type": "FuxaServer",
    "property": {},
    "tags": {},
    "enabled": True,
    "polling": 1000
}


def make_readonly(device):
    """Make all tags in a device read-only by removing write capability."""
    device = json.loads(json.dumps(device))  # Deep copy
    for tag_id, tag in device.get("tags", {}).items():
        tag["readonly"] = True
    return device


def make_safety_readonly(devices):
    """Make safety PLC read-only but keep process PLCs writable."""
    result = {}
    for dev_id, dev in devices.items():
        if dev_id == "sis301":
            result[dev_id] = make_readonly(dev)
        else:
            result[dev_id] = json.loads(json.dumps(dev))
    return result


def update_database(db_path, devices, project_name="Oil & Gas Process"):
    """Update FUXA database with device configurations."""
    conn = sqlite3.connect(db_path)
    cursor = conn.cursor()

    # Clear existing devices
    cursor.execute("DELETE FROM devices")

    # Insert FUXA Server
    cursor.execute(
        "INSERT OR REPLACE INTO devices (name, value) VALUES (?, ?)",
        ("0", json.dumps(FUXA_SERVER))
    )
    cursor.execute(
        "INSERT OR REPLACE INTO devices (name, value) VALUES (?, ?)",
        ("server", json.dumps({"id": "0", "name": "FUXA Server", "type": "FuxaServer", "property": {}}))
    )

    # Insert configured devices
    for dev_id, dev_config in devices.items():
        cursor.execute(
            "INSERT OR REPLACE INTO devices (name, value) VALUES (?, ?)",
            (dev_id, json.dumps(dev_config))
        )
        print(f"  Added device: {dev_config['name']} ({dev_id})")

    # Update general settings
    general = {
        "name": project_name,
        "language": "en",
        "projectVersion": "1.0.0"
    }
    cursor.execute(
        "INSERT OR REPLACE INTO general (name, value) VALUES (?, ?)",
        ("project", json.dumps(general))
    )

    conn.commit()
    conn.close()
    print(f"  Database updated: {db_path}")


def create_main_view():
    """Create a comprehensive main view with process overview."""
    view = {
        "id": "v_main",
        "name": "Oil & Gas Process Overview",
        "profile": {
            "width": 1600,
            "height": 1000,
            "bkcolor": "#0f172a",
            "margin": 10
        },
        "items": {}
    }

    items = view["items"]
    y_offset = 50

    # Title
    items["title"] = {
        "id": "title",
        "type": "svg-ext-text",
        "name": "OIL & GAS PRODUCTION FACILITY",
        "property": {
            "text": "OIL & GAS PRODUCTION FACILITY",
            "style": {"fontSize": 24, "fontWeight": "bold", "fill": "#38bdf8"}
        },
        "label": "Text"
    }

    # === SEPARATOR SECTION (PLC-101) ===
    section_y = y_offset + 60
    items["sep_title"] = create_text("sep_title", "SEPARATOR UNIT (PLC-101)", 50, section_y, "#f97316")

    # Separator tank visualization
    items["sep_tank"] = create_tank("sep_tank", "plc101", "sep_level", 100, section_y + 40, "Separator")

    # Pump controls
    items["pump_start"] = create_button("pump_start", "START", "plc101", "pump_cmd", "1", 250, section_y + 50, "#22c55e")
    items["pump_stop"] = create_button("pump_stop", "STOP", "plc101", "pump_cmd", "0", 250, section_y + 90, "#ef4444")
    items["pump_status"] = create_indicator("pump_status", "PUMP", "plc101", "running", 250, section_y + 130)
    items["pump_speed_val"] = create_value("pump_speed_val", "Speed", "plc101", "pump_speed", 250, section_y + 170, " RPM")

    # Valves
    items["oil_valve_ind"] = create_indicator("oil_valve_ind", "OIL VALVE", "plc101", "oil_valve", 380, section_y + 50)
    items["water_valve_ind"] = create_indicator("water_valve_ind", "WATER VALVE", "plc101", "water_valve", 380, section_y + 90)
    items["gas_valve_ind"] = create_indicator("gas_valve_ind", "GAS VALVE", "plc101", "gas_valve", 380, section_y + 130)

    # Process values
    items["sep_level_val"] = create_value("sep_level_val", "Level", "plc101", "sep_level", 100, section_y + 200, " %")
    items["sep_press_val"] = create_value("sep_press_val", "Pressure", "plc101", "sep_pressure", 100, section_y + 230, " PSI")

    # Manual mode toggle
    items["sep_manual"] = create_toggle("sep_manual", "MANUAL", "plc101", "manual_mode", 250, section_y + 210)

    # === COMPRESSOR SECTION (PLC-201) ===
    section_y = y_offset + 320
    items["comp_title"] = create_text("comp_title", "GAS COMPRESSOR (PLC-201)", 50, section_y, "#a855f7")

    # Compressor controls
    items["comp_start_btn"] = create_button("comp_start_btn", "START", "plc201", "comp_start", "1", 100, section_y + 50, "#22c55e")
    items["comp_stop_btn"] = create_button("comp_stop_btn", "STOP", "plc201", "comp_stop", "1", 100, section_y + 90, "#ef4444")

    # Status indicators
    items["comp_run_ind"] = create_indicator("comp_run_ind", "RUNNING", "plc201", "comp_running", 230, section_y + 50)
    items["comp_ready_ind"] = create_indicator("comp_ready_ind", "READY", "plc201", "comp_ready", 230, section_y + 90)
    items["comp_fault_ind"] = create_indicator("comp_fault_ind", "FAULT", "plc201", "fault", 230, section_y + 130, alarm=True)
    items["comp_alarm_ind"] = create_indicator("comp_alarm_ind", "ALARM", "plc201", "alarm", 230, section_y + 170, alarm=True)

    # Process values
    items["disch_press_val"] = create_value("disch_press_val", "Discharge", "plc201", "disch_press", 360, section_y + 50, " PSI")
    items["flow_val"] = create_value("flow_val", "Flow", "plc201", "flow", 360, section_y + 90, " SCFM")
    items["vib_val"] = create_value("vib_val", "Vibration", "plc201", "vibration", 360, section_y + 130, " mm/s")
    items["speed_sp_val"] = create_value("speed_sp_val", "Speed SP", "plc201", "speed_sp", 360, section_y + 170, " RPM")

    # Manual mode
    items["comp_manual"] = create_toggle("comp_manual", "MANUAL", "plc201", "manual_mode", 100, section_y + 170)

    # === SAFETY SYSTEM (SIS-301) ===
    section_y = y_offset + 60
    section_x = 550
    items["sis_title"] = create_text("sis_title", "SAFETY SYSTEM (SIS-301)", section_x, section_y, "#ef4444")

    # ESD Status (large indicator)
    items["esd_status"] = create_indicator("esd_status", "ESD ACTIVE", "sis301", "esd_active", section_x + 50, section_y + 50, alarm=True, large=True)

    # Safety outputs
    items["inlet_sdv_ind"] = create_indicator("inlet_sdv_ind", "INLET SDV", "sis301", "inlet_sdv", section_x, section_y + 120)
    items["outlet_sdv_ind"] = create_indicator("outlet_sdv_ind", "OUTLET SDV", "sis301", "outlet_sdv", section_x + 120, section_y + 120)
    items["blowdown_ind"] = create_indicator("blowdown_ind", "BLOWDOWN", "sis301", "blowdown", section_x + 240, section_y + 120)

    items["comp_trip_ind"] = create_indicator("comp_trip_ind", "COMP TRIP", "sis301", "comp_trip", section_x, section_y + 160)
    items["pump_trip_ind"] = create_indicator("pump_trip_ind", "PUMP TRIP", "sis301", "pump_trip", section_x + 120, section_y + 160)
    items["horn_ind"] = create_indicator("horn_ind", "HORN", "sis301", "horn", section_x + 240, section_y + 160, alarm=True)

    # Fire/Gas detection
    items["fire1_ind"] = create_indicator("fire1_ind", "FIRE 1", "sis301", "fire1", section_x, section_y + 220, alarm=True)
    items["fire2_ind"] = create_indicator("fire2_ind", "FIRE 2", "sis301", "fire2", section_x + 80, section_y + 220, alarm=True)
    items["gas1_ind"] = create_indicator("gas1_ind", "GAS 1", "sis301", "gas1", section_x + 160, section_y + 220, alarm=True)
    items["gas2_ind"] = create_indicator("gas2_ind", "GAS 2", "sis301", "gas2", section_x + 240, section_y + 220, alarm=True)

    # Gas PPM values
    items["gas_ppm1_val"] = create_value("gas_ppm1_val", "Gas PPM 1", "sis301", "gas_ppm1", section_x, section_y + 270, " ppm")
    items["gas_ppm2_val"] = create_value("gas_ppm2_val", "Gas PPM 2", "sis301", "gas_ppm2", section_x + 150, section_y + 270, " ppm")

    # === UTILITIES (PLC-401) ===
    section_y = y_offset + 320
    section_x = 550
    items["util_title"] = create_text("util_title", "UTILITIES (PLC-401)", section_x, section_y, "#22c55e")

    # Water Treatment
    items["wt_tank"] = create_tank("wt_tank", "plc401", "wt_level", section_x, section_y + 50, "Water Treatment")
    items["wt_pump_btn"] = create_button("wt_pump_btn", "WT PUMP", "plc401", "wt_pump", "1", section_x + 150, section_y + 60, "#3b82f6")
    items["wt_pump_ind"] = create_indicator("wt_pump_ind", "WT PUMP", "plc401", "wt_pump_fb", section_x + 150, section_y + 100)
    items["wt_level_val"] = create_value("wt_level_val", "WT Level", "plc401", "wt_level", section_x, section_y + 200, " %")
    items["wt_ph_val"] = create_value("wt_ph_val", "pH", "plc401", "wt_ph", section_x + 100, section_y + 200, "")

    # Oil Tank Farm
    items["oil_tank"] = create_tank("oil_tank", "plc401", "oil_level", section_x + 280, section_y + 50, "Oil Storage")
    items["oil_level_val"] = create_value("oil_level_val", "Oil Level", "plc401", "oil_level", section_x + 280, section_y + 200, " %")
    items["xfer_pump_ind"] = create_indicator("xfer_pump_ind", "XFER PUMP", "plc401", "xfer_pump", section_x + 280, section_y + 230)
    items["load_permit_ind"] = create_indicator("load_permit_ind", "LOAD OK", "plc401", "load_permit", section_x + 380, section_y + 230)

    # Alarms
    items["wt_alarm_ind"] = create_indicator("wt_alarm_ind", "WT ALARM", "plc401", "wt_alarm", section_x + 150, section_y + 140, alarm=True)
    items["tank_alarm_ind"] = create_indicator("tank_alarm_ind", "TANK ALARM", "plc401", "tank_alarm", section_x + 380, section_y + 140, alarm=True)

    # Manual mode
    items["util_manual"] = create_toggle("util_manual", "MANUAL", "plc401", "manual_mode", section_x + 150, section_y + 180)

    return view


def create_text(item_id, text, x, y, color="#94a3b8"):
    """Create a text label."""
    return {
        "id": item_id,
        "type": "svg-ext-text",
        "name": text,
        "x": x,
        "y": y,
        "property": {
            "text": text,
            "style": {"fontSize": 14, "fontWeight": "bold", "fill": color}
        },
        "label": "Text"
    }


def create_button(item_id, text, device, tag, value, x, y, color="#3b82f6"):
    """Create a clickable button."""
    return {
        "id": item_id,
        "type": "svg-ext-html_button",
        "name": text,
        "x": x,
        "y": y,
        "property": {
            "events": [{"type": "click", "action": "onSetValue", "actparam": value}],
            "variable": tag,
            "variableId": f"{device}^~^{tag}",
            "variableSrc": device,
            "style": {"backgroundColor": color, "color": "#ffffff", "borderRadius": "4px", "padding": "8px 16px"}
        },
        "label": "HtmlButton"
    }


def create_indicator(item_id, text, device, tag, x, y, alarm=False, large=False):
    """Create a status indicator (semaphore)."""
    off_color = "#555555"
    on_color = "#ef4444" if alarm else "#22c55e"
    return {
        "id": item_id,
        "type": "svg-ext-gauge_semaphore",
        "name": text,
        "x": x,
        "y": y,
        "property": {
            "variable": tag,
            "variableId": f"{device}^~^{tag}",
            "variableSrc": device,
            "ranges": [
                {"type": "range", "min": "0", "max": "0", "color": off_color},
                {"type": "range", "min": "1", "max": "1", "color": on_color}
            ],
            "style": {"width": 80 if large else 60, "height": 30 if large else 20}
        },
        "label": "HtmlSemaphore"
    }


def create_value(item_id, label, device, tag, x, y, unit=""):
    """Create a value display."""
    return {
        "id": item_id,
        "type": "svg-ext-value",
        "name": label,
        "x": x,
        "y": y,
        "property": {
            "variable": tag,
            "variableId": f"{device}^~^{tag}",
            "variableSrc": device,
            "ranges": [{"type": "unit", "min": 0, "max": 999, "text": unit}],
            "style": {"fontSize": 14, "fontWeight": "bold", "fill": "#e2e8f0"}
        },
        "label": "Value"
    }


def create_toggle(item_id, text, device, tag, x, y):
    """Create a toggle switch."""
    return {
        "id": item_id,
        "type": "svg-ext-html_switch",
        "name": text,
        "x": x,
        "y": y,
        "property": {
            "variable": tag,
            "variableId": f"{device}^~^{tag}",
            "variableSrc": device
        },
        "label": "HtmlSwitch"
    }


def create_tank(item_id, device, tag, x, y, label):
    """Create a tank level visualization."""
    return {
        "id": item_id,
        "type": "svg-ext-gauge_progress",
        "name": label,
        "x": x,
        "y": y,
        "property": {
            "variable": tag,
            "variableId": f"{device}^~^{tag}",
            "variableSrc": device,
            "min": 0,
            "max": 100,
            "ranges": [
                {"type": "range", "min": 0, "max": 20, "color": "#ef4444"},
                {"type": "range", "min": 20, "max": 80, "color": "#22c55e"},
                {"type": "range", "min": 80, "max": 100, "color": "#f59e0b"}
            ],
            "style": {"width": 80, "height": 150}
        },
        "label": "GaugeProgress"
    }


def update_view(db_path, view):
    """Update view in database."""
    conn = sqlite3.connect(db_path)
    cursor = conn.cursor()
    cursor.execute(
        "INSERT OR REPLACE INTO views (name, value) VALUES (?, ?)",
        (view["id"], json.dumps(view))
    )
    conn.commit()
    conn.close()


def main():
    print("=" * 60)
    print("RangerDanger FUXA HMI Configuration")
    print("=" * 60)

    # Paths
    view_db = os.path.join(BASE_DIR, "data", "fuxa_view_appdata", "project.fuxap.db")
    control_db = os.path.join(BASE_DIR, "data", "fuxa_control_appdata", "project.fuxap.db")

    # Configure HMI View (read-only for all devices)
    print("\n[HMI View - Read Only (DMZ)]")
    view_devices = {k: make_readonly(v) for k, v in DEVICES.items()}
    update_database(view_db, view_devices, "Oil & Gas Process (VIEW ONLY)")

    # Create main view for HMI View
    main_view = create_main_view()
    # Remove all buttons (make it view-only)
    main_view["items"] = {k: v for k, v in main_view["items"].items()
                          if v.get("type") not in ["svg-ext-html_button", "svg-ext-html_switch"]}
    update_view(view_db, main_view)
    print("  Added view-only process overview")

    # Configure HMI Control (full control for process PLCs - no access to safety zone)
    print("\n[HMI Control - Full Control (OT Zone)]")
    # Exclude SIS-301 since hmi_control can't access ot_safety_net
    control_devices = {k: v for k, v in DEVICES.items() if k != "sis301"}
    update_database(control_db, control_devices, "Oil & Gas Process (CONTROL)")

    # Create main view for HMI Control (with all controls)
    main_view = create_main_view()
    update_view(control_db, main_view)
    print("  Added full control process overview")

    print("\n" + "=" * 60)
    print("Configuration complete!")
    print("=" * 60)
    print("\nRestart HMI containers to apply changes:")
    print("  docker compose restart hmi_view hmi_control")


if __name__ == "__main__":
    main()
