-- Keep the built-in 4K presets in step with the SDR 4K scoring rule.
-- Copies people made from a preset keep their own scoring rules.
UPDATE pick_rules
SET body = json_insert(body, '$.traits[#]', json_object('trait', 'sdr_4k', 'points', -100))
WHERE preset = 1
  AND name IN ('4K', 'Best')
  AND json_type(body, '$.traits') = 'array'
  AND NOT EXISTS (
      SELECT 1 FROM json_each(pick_rules.body, '$.traits')
      WHERE json_extract(value, '$.trait') = 'sdr_4k'
  );
