"""Install and read back the local-only US OpenPLC demo."""
import pathlib
import sqlite3
import time
from contextlib import closing

ROOT = pathlib.Path("/root/OpenPLC_v3/webserver")
PROGRAM = "substation-automation.st"
SETTINGS = {
    "Start_run_mode": "true",
    "Slave_polling": "100",
    "Slave_timeout": "1000",
    "Modbus_port": "502",
    "Dnp3_port": "disabled",
    "Enip_port": "disabled",
}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def install():
    database = "file:" + str(ROOT / "openplc.db") + "?mode=rw"
    with closing(sqlite3.connect(database, uri=True)) as db:
        with db:
            db.execute("DELETE FROM Programs")
            db.execute(
                "INSERT INTO Programs (Name,Description,File,Date_upload) VALUES (?,?,?,?)",
                ("US feeder interlock demo", "Local-only OpenPLC seed", PROGRAM,
                 int(time.time())),
            )
            db.execute("DELETE FROM Slave_dev")
            for key, value in SETTINGS.items():
                db.execute(
                    "INSERT OR REPLACE INTO Settings (Key,Value) VALUES (?,?)",
                    (key, value),
                )

    # Verify the committed database state, including the absence of remote
    # Modbus master devices, before allowing the runtime to start.
    with closing(sqlite3.connect(
            "file:" + str(ROOT / "openplc.db") + "?mode=ro", uri=True)) as db:
        with db:
            require(db.execute("SELECT File FROM Programs").fetchall() == [(PROGRAM,)],
                    "program verification failed")
            require(db.execute("SELECT COUNT(*) FROM Slave_dev").fetchone()[0] == 0,
                    "remote Modbus master devices remain configured")
            actual = dict(db.execute("SELECT Key,Value FROM Settings"))
            require(all(actual.get(key) == value for key, value in SETTINGS.items()),
                    "settings verification failed")
    require((ROOT / "active_program").read_text().strip() == PROGRAM,
            "active program verification failed")

    config = 'Num_Devices = "0"\nPolling_Period = "100"\nTimeout = "1000"\n'
    (ROOT / "mbconfig.cfg").write_text(config)
    require((ROOT / "mbconfig.cfg").read_text() == config,
            "empty Modbus master configuration verification failed")


if __name__ == "__main__":
    install()
