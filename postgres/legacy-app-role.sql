\set ON_ERROR_STOP on
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

-- Select a fixed database and role pair before modifying any objects.
SELECT (current_database(), :'app_role') IN (
  ('chinwag', 'chinwag_app'), ('vita_track', 'vita_track_app')
) AS valid_target \gset
\if :valid_target
\else
DO $$ BEGIN RAISE EXCEPTION 'Unexpected database and role pair'; END $$;
\endif

SELECT format('CREATE ROLE %I LOGIN', :'app_role')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'app_role')
\gexec

-- Keep database ownership with the administrator. Prisma needs to own its
-- schema objects to alter them on future releases.
SELECT format('REVOKE CONNECT ON DATABASE %I FROM PUBLIC', current_database())
\gexec
SELECT format('GRANT CONNECT ON DATABASE %I TO %I', current_database(), :'app_role')
\gexec
SELECT format('GRANT USAGE, CREATE ON SCHEMA public TO %I', :'app_role')
\gexec
SELECT format('ALTER TABLE %I.%I OWNER TO %I', n.nspname, c.relname, :'app_role')
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
  AND c.relowner <> (SELECT oid FROM pg_roles WHERE rolname = :'app_role')
ORDER BY c.oid
\gexec
-- Changing table ownership also changes ownership of its owned sequences.
SELECT format('ALTER SEQUENCE %I.%I OWNER TO %I', n.nspname, c.relname, :'app_role')
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relkind = 'S'
  AND c.relowner <> (SELECT oid FROM pg_roles WHERE rolname = :'app_role')
ORDER BY c.oid
\gexec
COMMIT;
