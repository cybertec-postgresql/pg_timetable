-- 00798 Add timetable.secret store
-- Implements the Postgres-native secret store described in
-- spec/spec-design-secret-store.md (REQ-001..REQ-014, SEC-005, SEC-006,
-- PLT-002, CON-001, CON-007, DAT-001).

-- Ensure pgcrypto exists (REQ-007/REQ-008).
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_extension WHERE extname = 'pgcrypto') THEN
        EXECUTE 'CREATE EXTENSION pgcrypto SCHEMA timetable';
    END IF;
END;
$$;

CREATE TABLE timetable.secret (
    client_name  TEXT        NOT NULL,
    secret_name  TEXT        NOT NULL,
    value_enc    BYTEA       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   TEXT        NOT NULL DEFAULT session_user,
    PRIMARY KEY (client_name, secret_name)
);

ALTER TABLE timetable.secret ADD CONSTRAINT secret_name_format
    CHECK (secret_name ~ '^[A-Za-z0-9_.-]+$');

COMMENT ON TABLE timetable.secret IS
    'Write-only, named secret values referenced from task parameters and connection strings as ${secret:name}, scoped to exactly one client_name. Modeled on GitHub Actions repository secrets: no plaintext read path, no rotation, no versioning, no cross-client sharing.';
COMMENT ON COLUMN timetable.secret.client_name IS
    'Owning client. Mandatory security boundary: resolvable only by the scheduler process running with this exact client_name (-c/--clientname). Unlike timetable.chain.client_name, NULL/global is not permitted.';
COMMENT ON COLUMN timetable.secret.secret_name IS
    'Reference key used in ${secret:name} syntax, unique within client_name. Case-sensitive, no whitespace (enforced by CHECK secret_name_format).';
COMMENT ON COLUMN timetable.secret.value_enc IS
    'pgcrypto-encrypted value. Decrypted only by timetable.resolve_secret() when supplied the key configured as PGTT_SECRET_KEY, which the database never stores.';
COMMENT ON COLUMN timetable.secret.updated_by IS
    'session_user that last inserted or updated this row, maintained by the secret_touch trigger.';

REVOKE ALL ON timetable.secret FROM PUBLIC;
-- No grants to other roles. Objects are owned by the schema-creating (scheduler)
-- role; a separate administrative role is an operator-managed GRANT.

CREATE OR REPLACE FUNCTION timetable.secret_touch() RETURNS trigger AS
$CODE$
BEGIN
    NEW.updated_at := now();
    NEW.updated_by := session_user;
    RETURN NEW;
END;
$CODE$
LANGUAGE plpgsql;

COMMENT ON FUNCTION timetable.secret_touch() IS
    'Keeps timetable.secret.updated_at/updated_by truthful on UPDATE';

CREATE TRIGGER secret_touch
    BEFORE UPDATE ON timetable.secret
    FOR EACH ROW EXECUTE PROCEDURE timetable.secret_touch();

-- Create resolve_secret with the decrypt call schema-qualified to whichever
-- schema pgcrypto actually occupies (REQ-008). The qualification must be
-- baked into the body at creation time (REQ-053).
DO $OUTER$
DECLARE
    v_ext_schema TEXT;
BEGIN
    SELECT n.nspname INTO v_ext_schema
    FROM pg_catalog.pg_extension e
    JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
    WHERE e.extname = 'pgcrypto';

    IF v_ext_schema IS NULL THEN
        RAISE EXCEPTION 'pgcrypto extension is required by timetable.secret but is not installed';
    END IF;

    EXECUTE format($SQL$
        CREATE OR REPLACE FUNCTION timetable.resolve_secret(p_name TEXT, p_client TEXT, p_key TEXT)
        RETURNS TEXT
        LANGUAGE sql
        SECURITY DEFINER
        STABLE
        STRICT
        SET search_path = pg_catalog, timetable
        AS $BODY$
            SELECT %I.pgp_sym_decrypt(value_enc, p_key)
            FROM timetable.secret
            WHERE client_name = p_client
              AND secret_name = p_name;
        $BODY$;
    $SQL$, v_ext_schema);
END;
$OUTER$;

COMMENT ON FUNCTION timetable.resolve_secret(TEXT, TEXT, TEXT) IS
    'Returns the decrypted value of one secret, or NULL when the (client_name, secret_name) pair does not exist. Raises when the key is wrong.';

REVOKE ALL ON FUNCTION timetable.resolve_secret(TEXT, TEXT, TEXT) FROM PUBLIC;

CREATE OR REPLACE FUNCTION timetable.secret_count()
RETURNS BIGINT
LANGUAGE sql
SECURITY DEFINER
STABLE
SET search_path = pg_catalog, timetable
AS $$
    SELECT count(*) FROM timetable.secret;
$$;

COMMENT ON FUNCTION timetable.secret_count() IS
    'Number of stored secrets, used by the scheduler startup check when no encryption key is configured. Exposes no secret material.';

REVOKE ALL ON FUNCTION timetable.secret_count() FROM PUBLIC;
