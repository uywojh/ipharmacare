CREATE TABLE IF NOT EXISTS patients (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  full_name TEXT NOT NULL CHECK(length(trim(full_name)) BETWEEN 1 AND 120),
  phone TEXT NOT NULL UNIQUE CHECK(length(trim(phone)) BETWEEN 5 AND 32),
  date_of_birth TEXT,
  sex TEXT NOT NULL DEFAULT '' CHECK(sex IN ('', 'female', 'male', 'other', 'undisclosed')),
  allergies TEXT NOT NULL DEFAULT '',
  emergency_contact TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
