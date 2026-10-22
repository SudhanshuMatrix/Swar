-- Server-wide settings changed through the admin API (maintenance mode,
-- the active popup).
CREATE TABLE app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Announcements the app can show, such as "update the app" or "support us".
-- At most one is active at a time (app_settings.active_popup).
CREATE TABLE popups (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kind        TEXT NOT NULL,
    title       TEXT NOT NULL,
    message     TEXT NOT NULL,
    button_text TEXT NOT NULL DEFAULT '',
    button_url  TEXT NOT NULL DEFAULT '',
    dismissible INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
