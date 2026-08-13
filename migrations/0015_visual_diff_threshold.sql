-- Per-project visual-diff sensitivity (docs/VISUAL_REGRESSION.md "Not
-- yet built" — per-test-case override and a perceptual/SSIM metric
-- remain out of scope). Defaults to the same value the previously-fixed
-- constant used, so every existing project behaves identically until
-- someone explicitly changes it.
ALTER TABLE projects ADD COLUMN visual_diff_threshold DOUBLE PRECISION NOT NULL DEFAULT 30.0;
