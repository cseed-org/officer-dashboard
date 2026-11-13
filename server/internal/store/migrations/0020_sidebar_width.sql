-- Left nav sidebar width in px (V0.9 resizable sidebar). 0 = never resized (use the default).
ALTER TABLE user_prefs ADD COLUMN sidebar_width INTEGER NOT NULL DEFAULT 0;
