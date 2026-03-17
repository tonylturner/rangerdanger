#!/usr/bin/env python3
"""Configure FUXA HMI for the Substation Segmentation Lab.

Sets up a ModbusTCP device pointing to the RTAC simulator and creates
a substation one-line diagram view with live status indicators.

The RTAC exposes aggregated substation state via Modbus TCP on port 502.
See services/rtac-sim/modbus.go for the full register map.

Usage:
    python3 scripts/configure-fuxa-substation.py
    # Then restart FUXA:
    docker compose restart fuxa_hmi
"""

import json
import sqlite3
import os
import sys

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# FUXA Server device entry (always required)
FUXA_SERVER = {
    "id": "0",
    "name": "FUXA Server",
    "type": "FuxaServer",
    "enabled": True,
    "property": {},
    "tags": {}
}

# RTAC Modbus TCP device — points to rtac-sim on ot_ops_net
# FUXA and RTAC are both on ot_ops_net (10.30.30.x)
RTAC_DEVICE = {
    "id": "rtac",
    "name": "RTAC Supervisory Controller",
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
        # ── Coils (FC1) — Binary status ──
        # memaddress 000000 = Coils, FUXA uses 1-based addressing
        "breaker_closed": {
            "id": "breaker_closed", "name": "Breaker Closed",
            "address": "1", "type": "Bool", "memaddress": "000000", "readonly": True
        },
        "recloser_closed": {
            "id": "recloser_closed", "name": "Recloser Closed",
            "address": "2", "type": "Bool", "memaddress": "000000", "readonly": True
        },
        "gen_load_energized": {
            "id": "gen_load_energized", "name": "General Load Energized",
            "address": "3", "type": "Bool", "memaddress": "000000", "readonly": True
        },
        "crit_load_energized": {
            "id": "crit_load_energized", "name": "Critical Load Energized",
            "address": "4", "type": "Bool", "memaddress": "000000", "readonly": True
        },
        "reclose_enabled": {
            "id": "reclose_enabled", "name": "Reclose Enabled",
            "address": "5", "type": "Bool", "memaddress": "000000", "readonly": True
        },
        "remote_ctrl": {
            "id": "remote_ctrl", "name": "Remote Control Enabled",
            "address": "6", "type": "Bool", "memaddress": "000000", "readonly": True
        },
        "reg_auto_mode": {
            "id": "reg_auto_mode", "name": "Regulator Auto Mode",
            "address": "7", "type": "Bool", "memaddress": "000000", "readonly": True
        },
        "relay_comms": {
            "id": "relay_comms", "name": "Relay Comms OK",
            "address": "8", "type": "Bool", "memaddress": "000000", "readonly": True
        },
        "recloser_comms": {
            "id": "recloser_comms", "name": "Recloser Comms OK",
            "address": "9", "type": "Bool", "memaddress": "000000", "readonly": True
        },
        "regulator_comms": {
            "id": "regulator_comms", "name": "Regulator Comms OK",
            "address": "10", "type": "Bool", "memaddress": "000000", "readonly": True
        },

        # ── Discrete Inputs (FC2) — Alarms ──
        # memaddress 100000 = Discrete Inputs
        "alm_comm_loss": {
            "id": "alm_comm_loss", "name": "ALARM: Comm Loss",
            "address": "1", "type": "Bool", "memaddress": "100000", "readonly": True
        },
        "alm_breaker_open": {
            "id": "alm_breaker_open", "name": "ALARM: Breaker Open",
            "address": "2", "type": "Bool", "memaddress": "100000", "readonly": True
        },
        "alm_reclose_disabled": {
            "id": "alm_reclose_disabled", "name": "ALARM: Reclose Disabled",
            "address": "3", "type": "Bool", "memaddress": "100000", "readonly": True
        },
        "alm_low_voltage": {
            "id": "alm_low_voltage", "name": "ALARM: Low Voltage",
            "address": "4", "type": "Bool", "memaddress": "100000", "readonly": True
        },
        "alm_relay_fault": {
            "id": "alm_relay_fault", "name": "ALARM: Relay Fault",
            "address": "5", "type": "Bool", "memaddress": "100000", "readonly": True
        },
        "alm_relay_lockout": {
            "id": "alm_relay_lockout", "name": "ALARM: Relay Lockout",
            "address": "6", "type": "Bool", "memaddress": "100000", "readonly": True
        },
        "alm_recloser_fault": {
            "id": "alm_recloser_fault", "name": "ALARM: Recloser Fault",
            "address": "7", "type": "Bool", "memaddress": "100000", "readonly": True
        },
        "alm_recloser_lockout": {
            "id": "alm_recloser_lockout", "name": "ALARM: Recloser Lockout",
            "address": "8", "type": "Bool", "memaddress": "100000", "readonly": True
        },
        "alm_regulator_alarm": {
            "id": "alm_regulator_alarm", "name": "ALARM: Regulator",
            "address": "9", "type": "Bool", "memaddress": "100000", "readonly": True
        },

        # ── Holding Registers (FC3) — Device state ──
        # memaddress 400000 = Holding Registers
        "hr_breaker_closed": {
            "id": "hr_breaker_closed", "name": "HR Breaker Closed",
            "address": "1", "type": "Int16", "memaddress": "400000", "readonly": True
        },
        "hr_recloser_closed": {
            "id": "hr_recloser_closed", "name": "HR Recloser Closed",
            "address": "2", "type": "Int16", "memaddress": "400000", "readonly": True
        },
        "hr_reclose_enabled": {
            "id": "hr_reclose_enabled", "name": "HR Reclose Enabled",
            "address": "3", "type": "Int16", "memaddress": "400000", "readonly": True
        },
        "hr_tap_position": {
            "id": "hr_tap_position", "name": "Tap Position",
            "address": "4", "type": "Int16", "memaddress": "400000", "readonly": True
        },
        "hr_manual_mode": {
            "id": "hr_manual_mode", "name": "HR Manual Mode",
            "address": "5", "type": "Int16", "memaddress": "400000", "readonly": True
        },
        "hr_shot_count": {
            "id": "hr_shot_count", "name": "Shot Count",
            "address": "6", "type": "Int16", "memaddress": "400000", "readonly": True
        },

        # ── Input Registers (FC4) — Analog measurements (x10) ──
        # memaddress 300000 = Input Registers
        "bus_voltage": {
            "id": "bus_voltage", "name": "Bus Voltage (x10)",
            "address": "1", "type": "Int16", "memaddress": "300000", "readonly": True
        },
        "downstream_voltage": {
            "id": "downstream_voltage", "name": "Downstream Voltage (x10)",
            "address": "2", "type": "Int16", "memaddress": "300000", "readonly": True
        },
        "critical_voltage": {
            "id": "critical_voltage", "name": "Critical Load Voltage (x10)",
            "address": "3", "type": "Int16", "memaddress": "300000", "readonly": True
        },
        "feeder_current": {
            "id": "feeder_current", "name": "Feeder Current (x10)",
            "address": "4", "type": "Int16", "memaddress": "300000", "readonly": True
        },
        "gen_load_kw": {
            "id": "gen_load_kw", "name": "General Load kW",
            "address": "5", "type": "Int16", "memaddress": "300000", "readonly": True
        },
        "crit_load_kw": {
            "id": "crit_load_kw", "name": "Critical Load kW",
            "address": "6", "type": "Int16", "memaddress": "300000", "readonly": True
        },
        "relay_current": {
            "id": "relay_current", "name": "Relay Current (x10)",
            "address": "7", "type": "Int16", "memaddress": "300000", "readonly": True
        },
        "relay_voltage_kv": {
            "id": "relay_voltage_kv", "name": "Relay Voltage (x100)",
            "address": "8", "type": "Int16", "memaddress": "300000", "readonly": True
        },
    }
}


def update_database(db_path, devices, project_name):
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


# ── View element constructors ───────────────────────────────────────

def text(item_id, label, x, y, color="#94a3b8", size=14, bold=True):
    return {
        "id": item_id, "type": "svg-ext-text", "name": label,
        "x": x, "y": y,
        "property": {
            "text": label,
            "style": {"fontSize": size, "fontWeight": "bold" if bold else "normal", "fill": color}
        },
        "label": "Text"
    }


def indicator(item_id, label, tag, x, y, alarm=False, large=False):
    off_color = "#555555"
    on_color = "#ef4444" if alarm else "#22c55e"
    return {
        "id": item_id, "type": "svg-ext-gauge_semaphore", "name": label,
        "x": x, "y": y,
        "property": {
            "variable": tag,
            "variableId": f"rtac^~^{tag}",
            "variableSrc": "rtac",
            "ranges": [
                {"type": "range", "min": "0", "max": "0", "color": off_color},
                {"type": "range", "min": "1", "max": "1", "color": on_color}
            ],
            "style": {"width": 100 if large else 70, "height": 30 if large else 22}
        },
        "label": "HtmlSemaphore"
    }


def value_display(item_id, label, tag, x, y, unit="", scale=1):
    return {
        "id": item_id, "type": "svg-ext-value", "name": label,
        "x": x, "y": y,
        "property": {
            "variable": tag,
            "variableId": f"rtac^~^{tag}",
            "variableSrc": "rtac",
            "ranges": [{"type": "unit", "min": 0, "max": 9999, "text": unit}],
            "style": {"fontSize": 16, "fontWeight": "bold", "fill": "#e2e8f0"}
        },
        "label": "Value"
    }


def gauge(item_id, label, tag, x, y, min_val=0, max_val=1500, ranges=None):
    if ranges is None:
        ranges = [
            {"type": "range", "min": 0, "max": 1080, "color": "#ef4444"},
            {"type": "range", "min": 1080, "max": 1140, "color": "#f59e0b"},
            {"type": "range", "min": 1140, "max": 1260, "color": "#22c55e"},
            {"type": "range", "min": 1260, "max": 1320, "color": "#f59e0b"},
            {"type": "range", "min": 1320, "max": 1500, "color": "#ef4444"},
        ]
    return {
        "id": item_id, "type": "svg-ext-gauge_progress", "name": label,
        "x": x, "y": y,
        "property": {
            "variable": tag,
            "variableId": f"rtac^~^{tag}",
            "variableSrc": "rtac",
            "min": min_val, "max": max_val,
            "ranges": ranges,
            "style": {"width": 60, "height": 120}
        },
        "label": "GaugeProgress"
    }


# ── Main view: Substation One-Line Diagram ──────────────────────────

def create_substation_view():
    """Create the main substation one-line diagram view."""
    view = {
        "id": "v_substation",
        "name": "Substation One-Line Diagram",
        "profile": {
            "width": 1400,
            "height": 900,
            "bkcolor": "#0f172a",
            "margin": 10
        },
        "items": {}
    }
    items = view["items"]

    # ── Title ──
    items["title"] = text("title", "DISTRIBUTION SUBSTATION — ONE-LINE DIAGRAM", 50, 30, "#38bdf8", 22)
    items["subtitle"] = text("subtitle", "Electric Cooperative — Feeder 101", 50, 58, "#64748b", 12, False)

    # ── Source / Bus Section ──
    items["src_label"] = text("src_label", "SUBSTATION BUS", 80, 100, "#38bdf8", 16)
    items["bus_v_label"] = text("bus_v_label", "Bus Voltage:", 80, 130, "#94a3b8", 12, False)
    items["bus_v_val"] = value_display("bus_v_val", "Bus V", "bus_voltage", 175, 126, " (x10 V)")
    items["bus_kv_label"] = text("bus_kv_label", "12.47 kV Nominal", 80, 150, "#64748b", 11, False)

    # ── Feeder Breaker / Relay ──
    items["bkr_section"] = text("bkr_section", "FEEDER BREAKER (52)", 350, 100, "#f97316", 16)
    items["bkr_status"] = indicator("bkr_status", "CLOSED", "breaker_closed", 350, 130, large=True)
    items["bkr_fault"] = indicator("bkr_fault", "FAULT", "alm_relay_fault", 460, 130, alarm=True)
    items["bkr_lockout"] = indicator("bkr_lockout", "LOCKOUT", "alm_relay_lockout", 540, 130, alarm=True)
    items["bkr_remote"] = indicator("bkr_remote", "REMOTE", "remote_ctrl", 350, 160)
    items["relay_i_label"] = text("relay_i_label", "Current:", 460, 162, "#94a3b8", 11, False)
    items["relay_i_val"] = value_display("relay_i_val", "I", "relay_current", 520, 158, " (x10 A)")

    # ── Main Feeder ──
    items["feeder_label"] = text("feeder_label", "MAIN FEEDER", 350, 210, "#64748b", 12)
    items["feeder_i_label"] = text("feeder_i_label", "Feeder Current:", 350, 232, "#94a3b8", 11, False)
    items["feeder_i_val"] = value_display("feeder_i_val", "Feeder I", "feeder_current", 460, 228, " (x10 A)")
    items["ds_v_label"] = text("ds_v_label", "Downstream V:", 350, 252, "#94a3b8", 11, False)
    items["ds_v_val"] = value_display("ds_v_val", "DS V", "downstream_voltage", 460, 248, " (x10 V)")

    # ── Recloser ──
    items["rcl_section"] = text("rcl_section", "RECLOSER (79)", 350, 300, "#a855f7", 16)
    items["rcl_status"] = indicator("rcl_status", "CLOSED", "recloser_closed", 350, 330, large=True)
    items["rcl_reclose"] = indicator("rcl_reclose", "AUTO-RECLOSE", "reclose_enabled", 460, 330)
    items["rcl_fault"] = indicator("rcl_fault", "FAULT", "alm_recloser_fault", 560, 330, alarm=True)
    items["rcl_lockout"] = indicator("rcl_lockout", "LOCKOUT", "alm_recloser_lockout", 640, 330, alarm=True)
    items["rcl_shots_label"] = text("rcl_shots_label", "Shots:", 350, 362, "#94a3b8", 11, False)
    items["rcl_shots_val"] = value_display("rcl_shots_val", "Shots", "hr_shot_count", 400, 358, "")

    # ── Branch A: General Load ──
    items["branch_a"] = text("branch_a", "BRANCH A — GENERAL LOAD", 80, 420, "#22c55e", 14)
    items["gen_load_ind"] = indicator("gen_load_ind", "ENERGIZED", "gen_load_energized", 80, 450, large=True)
    items["gen_kw_label"] = text("gen_kw_label", "Load:", 80, 485, "#94a3b8", 12, False)
    items["gen_kw_val"] = value_display("gen_kw_val", "Gen kW", "gen_load_kw", 130, 481, " kW")

    # ── Branch B: Critical Load + Regulator ──
    items["branch_b"] = text("branch_b", "BRANCH B — CRITICAL LOAD", 400, 420, "#ef4444", 14)

    # Voltage Regulator
    items["reg_section"] = text("reg_section", "VOLTAGE REGULATOR (90)", 400, 450, "#f59e0b", 13)
    items["reg_auto"] = indicator("reg_auto", "AUTO", "reg_auto_mode", 400, 475)
    items["reg_tap_label"] = text("reg_tap_label", "Tap:", 480, 478, "#94a3b8", 12, False)
    items["reg_tap_val"] = value_display("reg_tap_val", "Tap", "hr_tap_position", 520, 474, "")
    items["reg_alarm"] = indicator("reg_alarm", "ALARM", "alm_regulator_alarm", 580, 475, alarm=True)

    # Critical Load
    items["crit_load_ind"] = indicator("crit_load_ind", "ENERGIZED", "crit_load_energized", 400, 510, large=True)
    items["crit_kw_label"] = text("crit_kw_label", "Load:", 400, 545, "#94a3b8", 12, False)
    items["crit_kw_val"] = value_display("crit_kw_val", "Crit kW", "crit_load_kw", 450, 541, " kW")
    items["crit_v_label"] = text("crit_v_label", "Voltage:", 530, 545, "#94a3b8", 12, False)
    items["crit_v_val"] = value_display("crit_v_val", "Crit V", "critical_voltage", 590, 541, " (x10 V)")

    # ── Voltage Gauge ──
    items["v_gauge_label"] = text("v_gauge_label", "CRITICAL LOAD VOLTAGE", 750, 420, "#f59e0b", 13)
    items["v_gauge"] = gauge("v_gauge", "Voltage Gauge", "critical_voltage", 780, 450)
    items["v_range_a"] = text("v_range_a", "Range A: 1140-1260 (x10)", 860, 470, "#22c55e", 10, False)
    items["v_range_b"] = text("v_range_b", "Range B: 1080-1320 (x10)", 860, 488, "#f59e0b", 10, False)
    items["v_danger"] = text("v_danger", "Danger: <1080 or >1320", 860, 506, "#ef4444", 10, False)

    # ── Communications Status ──
    items["comms_section"] = text("comms_section", "DEVICE COMMUNICATIONS", 80, 610, "#38bdf8", 14)
    items["relay_comm"] = indicator("relay_comm", "RELAY", "relay_comms", 80, 640)
    items["recloser_comm"] = indicator("recloser_comm", "RECLOSER", "recloser_comms", 170, 640)
    items["regulator_comm"] = indicator("regulator_comm", "REGULATOR", "regulator_comms", 280, 640)
    items["comm_loss_alarm"] = indicator("comm_loss_alarm", "COMM LOSS", "alm_comm_loss", 400, 640, alarm=True)

    # ── Alarm Summary ──
    items["alarm_section"] = text("alarm_section", "ALARM SUMMARY", 550, 610, "#ef4444", 14)
    items["alm_bkr"] = indicator("alm_bkr", "BKR OPEN", "alm_breaker_open", 550, 640, alarm=True)
    items["alm_rcl"] = indicator("alm_rcl", "RECLOSE OFF", "alm_reclose_disabled", 650, 640, alarm=True)
    items["alm_lv"] = indicator("alm_lv", "LOW VOLTAGE", "alm_low_voltage", 760, 640, alarm=True)

    # ── Legend ──
    items["legend_label"] = text("legend_label", "Note: Voltage values are x10 scaled (1200 = 120.0V). Tap range: -16 to +16.", 80, 720, "#475569", 11, False)

    return view


def create_alarm_view():
    """Create an alarm summary view."""
    view = {
        "id": "v_alarms",
        "name": "Alarm Summary",
        "profile": {
            "width": 1000,
            "height": 600,
            "bkcolor": "#0f172a",
            "margin": 10
        },
        "items": {}
    }
    items = view["items"]

    items["title"] = text("title", "SUBSTATION ALARM SUMMARY", 50, 30, "#ef4444", 22)

    # Protection Alarms
    items["prot_section"] = text("prot_section", "PROTECTION ALARMS", 50, 80, "#f97316", 16)
    y = 110
    for tag, label in [
        ("alm_relay_fault", "Relay Fault Detected"),
        ("alm_relay_lockout", "Relay Lockout"),
        ("alm_recloser_fault", "Recloser Fault Detected"),
        ("alm_recloser_lockout", "Recloser Lockout"),
        ("alm_breaker_open", "Breaker Open (Unexpected)"),
    ]:
        items[tag] = indicator(tag, label, tag, 70, y, alarm=True, large=True)
        items[f"{tag}_lbl"] = text(f"{tag}_lbl", label, 190, y + 4, "#e2e8f0", 13, False)
        y += 40

    # Operational Alarms
    items["ops_section"] = text("ops_section", "OPERATIONAL ALARMS", 500, 80, "#f59e0b", 16)
    y = 110
    for tag, label in [
        ("alm_reclose_disabled", "Auto-Reclose Disabled"),
        ("alm_low_voltage", "Low Voltage — Critical Load"),
        ("alm_regulator_alarm", "Regulator Alarm"),
        ("alm_comm_loss", "Device Communication Loss"),
    ]:
        items[tag + "_2"] = indicator(tag + "_2", label, tag, 520, y, alarm=True, large=True)
        items[f"{tag}_lbl2"] = text(f"{tag}_lbl2", label, 640, y + 4, "#e2e8f0", 13, False)
        y += 40

    # Status
    items["status_section"] = text("status_section", "DEVICE STATUS", 50, 360, "#22c55e", 16)
    y = 390
    for tag, label in [
        ("breaker_closed", "Feeder Breaker Closed"),
        ("recloser_closed", "Recloser Closed"),
        ("reclose_enabled", "Auto-Reclose Enabled"),
        ("reg_auto_mode", "Regulator Auto Mode"),
    ]:
        items[tag + "_st"] = indicator(tag + "_st", label, tag, 70, y, large=True)
        items[f"{tag}_stlbl"] = text(f"{tag}_stlbl", label, 190, y + 4, "#e2e8f0", 13, False)
        y += 35

    return view


def main():
    print("=" * 60)
    print("RangerDanger — FUXA Substation HMI Configuration")
    print("=" * 60)

    db_path = os.path.join(BASE_DIR, "data", "fuxa_hmi_appdata", "project.fuxap.db")

    if not os.path.exists(db_path):
        print(f"\nERROR: Database not found: {db_path}")
        print("Make sure FUXA HMI container has started at least once.")
        print("Run: docker compose up -d fuxa_hmi")
        sys.exit(1)

    # Configure RTAC device
    print(f"\n[Configuring FUXA HMI — Substation View]")
    devices = {"rtac": RTAC_DEVICE}
    update_database(db_path, devices, "Substation Segmentation Lab")

    # Create views
    oneline = create_substation_view()
    update_view(db_path, oneline)
    print("  Added view: Substation One-Line Diagram")

    alarms = create_alarm_view()
    update_view(db_path, alarms)
    print("  Added view: Alarm Summary")

    print("\n" + "=" * 60)
    print("Configuration complete!")
    print("=" * 60)
    print("\nRestart FUXA to apply changes:")
    print("  docker compose restart fuxa_hmi")
    print("\nAccess FUXA HMI at:")
    print("  http://localhost:8088/apps/fuxa-hmi/")
    print("  (or directly at the container: http://10.30.30.10:1881)")


if __name__ == "__main__":
    main()
