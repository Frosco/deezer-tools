# deezer-tools

Personal toolbox for Deezer account automation.

> **Heads up:** this tool talks to Deezer's *unofficial* `gw-light.php` gateway.
> Deezer no longer accepts new app registrations on their developer portal,
> so the documented OAuth API is closed. The unofficial gateway is what
> every open-source Deezer tool uses today; it can change without notice.

## Setup

1. Build:
   ```sh
   go build -o deezer-tools ./cmd/deezer-tools
   ```

2. Get your `arl` cookie:
   - Log in at https://www.deezer.com.
   - Open DevTools → Application → Cookies → `https://www.deezer.com`.
   - Copy the value of the `arl` cookie.

3. Create the config file:
   ```sh
   mkdir -p ~/.config/deezer-tools
   printf 'arl = "PASTE_VALUE_HERE"\n' > ~/.config/deezer-tools/config.toml
   chmod 600 ~/.config/deezer-tools/config.toml
   ```

The tool refuses to start if the config file is world-readable.

## Commands

### `loved-tracks wipe`

Delete every loved track. Loved albums and loved artists are **not** touched.

```sh
# Dry-run: list and back up, do not delete.
deezer-tools loved-tracks wipe --dry-run --backup-dir ./backups

# Real run: lists, writes backup, asks for the count to confirm, deletes.
deezer-tools loved-tracks wipe --backup-dir ./backups
```

The backup is a JSON array at `<backup-dir>/deezer-loved-tracks-<UTC-timestamp>.json`
containing every song's ID, title, artist, album, and date_added.

If any individual track fails to delete with a permanent error, the run
continues, the failing tracks are recorded in `<same-prefix>.skip.log`, and
the process exits non-zero so you can review them.

If your `arl` is invalid, the run aborts immediately.
