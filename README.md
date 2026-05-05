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

### `playlists love-contents`

For one or more Deezer playlists, love every album and artist whose songs
appear in them. Already-loved items are no-ops. Use this to expand your
loved-albums and loved-artists collections from playlists that contain
complete albums.

```sh
deezer-tools playlists love-contents [--dry-run] [--backup-dir <dir>] <input>...
```

Each `<input>` may be:

- A bare numeric playlist ID: `15018766163`
- A full Deezer playlist URL: `https://www.deezer.com/en/playlist/15018766163`
- A short share link: `https://link.deezer.com/s/337D7rZEQd0wiR1D0ivjS`

If no positional args are given, inputs are read from stdin (one per line,
blank lines and `#` comments ignored). The confirm prompt then reads from
`/dev/tty`.

A JSON run record is written to
`<backup-dir>/deezer-playlist-love-<UTC-timestamp>.json` before the apply
phase. Per-item failures append one JSON line to
`<backup-dir>/deezer-playlist-love-<UTC-timestamp>.skip.log` and the process
exits non-zero so you can review them.

Sequential paced writes (1s ± 200ms between attempts, 5s/15s/30s/60s/120s
retry on rate-limit/5xx) protect against the gw-light quota that
historically tripped Akamai's WAF on the wipe — see
`docs/solutions/integration-issues/`.

### `loved-albums dedupe`

Find and remove duplicate entries in your loved-albums list:

- **Case 1** — same artist, same normalised title, different ALB_IDs. Picks
  the album with most tracks → most fans → lowest ID; un-loves the rest.
- **Case 2** — a short loved album (default ≤3 tracks) whose title equals a
  track on a longer same-artist album that's also loved. Un-loves the short
  one; the longer album stays loved.

```sh
deezer-tools loved-albums dedupe --dry-run            # detect, write report, do not unlove
deezer-tools loved-albums dedupe --backup-dir ./out   # write run record + skip log to ./out
deezer-tools loved-albums dedupe --case2-track-threshold 5
```

After detection a run record is written to
`<backup-dir>/deezer-loved-albums-dedupe-<UTC>.json`. After a single batched
confirmation, losers are un-loved with the same paced-throttle / retry /
circuit-breaker discipline as `loved-tracks wipe` and `playlists love-contents`.
Run record and skip log are gitignored.

`playlists love-contents` also gains a within-playlist Case-1 collapse: when
two songs in the source playlist resolve to different ALB_IDs for the same
album, only the canonical edition is loved. Across-playlist Case-1 dedup is
left to the standalone `loved-albums dedupe` command.
