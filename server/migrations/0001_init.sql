-- Devices are anonymous installations identified by a server-issued ID.
CREATE TABLE devices (
    id           TEXT PRIMARY KEY,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
);

-- Snapshot of track metadata so libraries render without upstream calls.
CREATE TABLE track_meta (
    track_id   TEXT PRIMARY KEY,
    data       TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE likes (
    device_id  TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    track_id   TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (device_id, track_id)
);

CREATE TABLE dislikes (
    device_id  TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    track_id   TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (device_id, track_id)
);

CREATE TABLE playlists (
    id         TEXT PRIMARY KEY,
    device_id  TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    name       TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX playlists_by_device ON playlists(device_id, created_at);

CREATE TABLE playlist_tracks (
    playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL CHECK (position >= 0),
    track_id    TEXT NOT NULL,
    added_at    INTEGER NOT NULL,
    PRIMARY KEY (playlist_id, position)
);

CREATE TABLE history (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id  TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    track_id   TEXT NOT NULL,
    played_at  INTEGER NOT NULL,
    ms_played  INTEGER NOT NULL DEFAULT 0 CHECK (ms_played >= 0),
    skipped    INTEGER NOT NULL DEFAULT 0 CHECK (skipped IN (0, 1)),
    context    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX history_by_device_time ON history(device_id, played_at DESC);

CREATE TABLE preferences (
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    key       TEXT NOT NULL,
    value     TEXT NOT NULL,
    PRIMARY KEY (device_id, key)
);

-- Client-reported state of offline copies. Audio never touches the server.
CREATE TABLE downloads (
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    track_id    TEXT NOT NULL,
    local_id    TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('queued','downloading','paused','verifying','completed','failed','canceled')),
    bytes_total INTEGER NOT NULL DEFAULT 0 CHECK (bytes_total >= 0),
    bytes_done  INTEGER NOT NULL DEFAULT 0 CHECK (bytes_done >= 0),
    sha256      TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (device_id, track_id)
);

CREATE TABLE queue_state (
    device_id  TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    data       TEXT NOT NULL,
    revision   INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
