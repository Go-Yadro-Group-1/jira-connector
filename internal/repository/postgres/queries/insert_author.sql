INSERT INTO raw.author (id, name)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;
