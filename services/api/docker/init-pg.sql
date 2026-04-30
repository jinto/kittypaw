-- Bootstrap test database with pg_trgm extension. Run by postgres image
-- via docker-entrypoint-initdb.d on first container start.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
