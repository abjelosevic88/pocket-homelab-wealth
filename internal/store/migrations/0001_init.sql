CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);

-- ---------- Pocket Wealth: everything below is per person ----------

-- Something you own or owe: a bank account, a portfolio, a pension, the flat,
-- the car, a loan. Its value comes from its valuations (manual), the sum of
-- its positions, the app it is linked to (Pocket Money, Garage, Inventory),
-- or an estimate that grows or shrinks from the last known value.
CREATE TABLE assets (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  kind          TEXT NOT NULL DEFAULT 'other',   -- cash | savings | deposit | investment | pension | crypto | property | vehicle | valuables | business | receivable | other | mortgage | loan | credit_card | debt
  currency      TEXT NOT NULL DEFAULT 'EUR',
  institution   TEXT NOT NULL DEFAULT '',        -- the bank, the broker, the pension fund
  number        TEXT NOT NULL DEFAULT '',        -- account, policy or contract number
  notes         TEXT NOT NULL DEFAULT '',
  value_mode    TEXT NOT NULL DEFAULT 'manual',  -- manual | positions | linked | estimate
  include       INTEGER NOT NULL DEFAULT 1,      -- counted in net worth
  share         REAL NOT NULL DEFAULT 1,         -- the part that is yours (0.5 = half of a shared flat)
  rate          REAL NOT NULL DEFAULT 0,         -- interest % a year (a deposit, a loan)
  payment       INTEGER NOT NULL DEFAULT 0,      -- minor units a month: the loan payment, or what you put in
  growth        REAL,                            -- expected change % a year (NULL = the kind's default); the estimate uses it too
  matures_on    TEXT NOT NULL DEFAULT '',        -- a deposit matures, a fixed rate ends, the last loan payment
  bought_on     TEXT NOT NULL DEFAULT '',
  bought_price  INTEGER,                         -- minor units: what it cost (a flat, a car)
  invested      INTEGER,                         -- minor units: what you put in, for the gain (NULL = not known)
  source        TEXT NOT NULL DEFAULT 'web',     -- web | api | import | demo | money | garage | home | inventory | <app slug>
  source_ref    TEXT NOT NULL DEFAULT '',        -- "money:account:4", "garage:vehicle:2", "home:home:1", "inventory:items"
  source_url    TEXT NOT NULL DEFAULT '',        -- open it in the app it comes from
  linked_value  INTEGER,                         -- the latest value read from that app (asset currency)
  linked_at     TEXT NOT NULL DEFAULT '',
  linked_error  TEXT NOT NULL DEFAULT '',
  closed_on     TEXT NOT NULL DEFAULT '',        -- sold, paid off, closed: not counted from this day
  sort          INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX assets_user ON assets(user_id, closed_on, sort);
CREATE UNIQUE INDEX assets_source ON assets(user_id, source_ref) WHERE source_ref <> '';

-- What something was worth on a day, in its own currency, all of it (the
-- share is applied on read). Debts are positive: what is owed.
CREATE TABLE valuations (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  asset_id   INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  date       TEXT NOT NULL,
  value      INTEGER NOT NULL,
  note       TEXT NOT NULL DEFAULT '',
  source     TEXT NOT NULL DEFAULT 'web',        -- web | auto (from positions or the linked app) | ai | import | demo | <app slug>
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (asset_id, date)
);
CREATE INDEX valuations_user ON valuations(user_id, asset_id, date);

-- A holding inside a portfolio, pension or crypto wallet: units at a price.
CREATE TABLE positions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  asset_id    INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  symbol      TEXT NOT NULL DEFAULT '',          -- ticker for the online price: VWCE.DE, AAPL, BTC-EUR
  kind        TEXT NOT NULL DEFAULT 'fund',      -- stock | etf | fund | bond | crypto | cash | other
  units       REAL NOT NULL DEFAULT 0,
  cost        INTEGER NOT NULL DEFAULT 0,        -- minor units: what all of them cost
  currency    TEXT NOT NULL DEFAULT 'EUR',
  price       REAL NOT NULL DEFAULT 0,           -- per unit, major units
  price_on    TEXT NOT NULL DEFAULT '',
  price_auto  INTEGER NOT NULL DEFAULT 1,        -- look the price up online (needs a symbol)
  price_error TEXT NOT NULL DEFAULT '',
  notes       TEXT NOT NULL DEFAULT '',
  sort        INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX positions_user ON positions(user_id, asset_id, sort);

-- Buys, sells, dividends, interest, money put in or taken out.
CREATE TABLE activity (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  asset_id    INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  position_id INTEGER REFERENCES positions(id) ON DELETE SET NULL,
  date        TEXT NOT NULL,
  kind        TEXT NOT NULL,                     -- buy | sell | dividend | interest | deposit | withdrawal | fee
  units       REAL NOT NULL DEFAULT 0,
  amount      INTEGER NOT NULL DEFAULT 0,        -- minor units, always positive
  currency    TEXT NOT NULL DEFAULT 'EUR',
  note        TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL DEFAULT 'web',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX activity_user ON activity(user_id, date);
CREATE INDEX activity_asset ON activity(asset_id, date);

-- A statement or contract kept with an asset: a document in Pocket
-- Documents, or a link.
CREATE TABLE attachments (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  asset_id    INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  document_id INTEGER,
  title       TEXT NOT NULL DEFAULT '',
  url         TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX attachments_asset ON attachments(user_id, asset_id);

-- Net worth milestones reached, so each is announced once.
CREATE TABLE milestones (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  amount     INTEGER NOT NULL,                   -- minor units of currency
  currency   TEXT NOT NULL,
  reached_on TEXT NOT NULL,
  PRIMARY KEY (user_id, amount, currency)
);

-- Suggestions from the other apps that were turned down: never offered again.
CREATE TABLE dismissed (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_ref TEXT NOT NULL,
  title      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, source_ref)
);

-- Exchange rates: units of a currency per euro on a day (the European
-- Central Bank's reference rates). Facts about the world, not anybody's
-- content, so shared by the whole box.
CREATE TABLE fx (
  currency TEXT NOT NULL,
  date     TEXT NOT NULL,
  per_eur  REAL NOT NULL,
  source   TEXT NOT NULL DEFAULT 'ecb',          -- ecb | yahoo | demo
  PRIMARY KEY (currency, date)
);
