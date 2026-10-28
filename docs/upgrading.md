# Rolling Back A Failed v1-to-v2 Upgrade

[Back to README](../README.md) · [简体中文](./upgrading.zh.md)

Keeper creates a database backup before migrating v1 data. If migration fails and that backup is verified, the logs include English rollback instructions and its `backup_path`.

1. Stop Keeper v2 and preserve the failed database together with any `-wal` and `-shm` files.
2. Copy the pre-upgrade backup to `<new WORK_DIR>/app.db` in a separate data directory, renaming the backup copy to `app.db`. Keep the original backup intact; do not mix the copy with files from the failed database.
3. Install Keeper v1 and set `WORK_DIR` to that new directory. With the default Docker setup, mount the new directory at `/data`, so the restored file is `/data/app.db` inside the container. Do not open the partially migrated database with v1.

This returns data to the backup point. Usage received afterward is not included. If no verified backup is available, locate and validate a known pre-upgrade backup before attempting rollback.
