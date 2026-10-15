CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY,
  username      TEXT    NOT NULL,
  display_name  TEXT    NOT NULL,
  password_hash TEXT    NOT NULL,
  avatar_key    TEXT,
  created_at    TEXT    NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users(username);

CREATE TABLE IF NOT EXISTS projects (
  id          INTEGER PRIMARY KEY,
  name        TEXT    NOT NULL,
  description TEXT    NOT NULL DEFAULT '',
  invite_code TEXT    NOT NULL,
  color       TEXT    NOT NULL DEFAULT '#6366f1',
  created_by  INTEGER NOT NULL REFERENCES users(id),
  created_at  TEXT    NOT NULL,
  updated_at  TEXT    NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_invite ON projects(invite_code);

CREATE TABLE IF NOT EXISTS project_members (
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
  role       TEXT    NOT NULL CHECK(role IN ('owner','member')),
  color      TEXT    NOT NULL,
  joined_at  TEXT    NOT NULL,
  PRIMARY KEY (project_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_pm_user ON project_members(user_id);

CREATE TABLE IF NOT EXISTS folders (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  parent_id  INTEGER REFERENCES folders(id) ON DELETE CASCADE,
  name       TEXT    NOT NULL,
  position   INTEGER NOT NULL DEFAULT 0,
  created_at TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_folders_parent ON folders(project_id, parent_id);

CREATE TABLE IF NOT EXISTS items (
  id            INTEGER PRIMARY KEY,
  project_id    INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  folder_id     INTEGER REFERENCES folders(id) ON DELETE CASCADE,
  kind          TEXT    NOT NULL CHECK(kind IN ('doc','file')),
  title         TEXT    NOT NULL,
  body_html     TEXT,
  storage_key   TEXT,
  original_name TEXT,
  mime          TEXT,
  size_bytes    INTEGER,
  created_by    INTEGER NOT NULL REFERENCES users(id),
  created_at    TEXT    NOT NULL,
  updated_at    TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_items_dir ON items(project_id, folder_id, kind);

CREATE TABLE IF NOT EXISTS calendar_entries (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
  day        TEXT    NOT NULL,
  content    TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL,
  updated_at TEXT    NOT NULL,
  UNIQUE (project_id, user_id, day)
);
CREATE INDEX IF NOT EXISTS idx_cal_month ON calendar_entries(project_id, day);

CREATE TABLE IF NOT EXISTS labels (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name       TEXT    NOT NULL,
  color      TEXT    NOT NULL,
  UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS cards (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title       TEXT    NOT NULL,
  description TEXT    NOT NULL DEFAULT '',
  assignee_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  status      TEXT    NOT NULL DEFAULT 'todo' CHECK(status IN ('todo','doing','done')),
  position    REAL    NOT NULL DEFAULT 1000,
  due_date    TEXT,
  created_by  INTEGER NOT NULL REFERENCES users(id),
  created_at  TEXT    NOT NULL,
  updated_at  TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cards_board ON cards(project_id, status, position);

CREATE TABLE IF NOT EXISTS card_labels (
  card_id  INTEGER NOT NULL REFERENCES cards(id)  ON DELETE CASCADE,
  label_id INTEGER NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
  PRIMARY KEY (card_id, label_id)
);

CREATE VIRTUAL TABLE IF NOT EXISTS search_index USING fts5(
  title, body,
  project_id UNINDEXED, kind UNINDEXED, ref_id UNINDEXED,
  tokenize='trigram'
);
